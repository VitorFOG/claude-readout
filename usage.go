package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// The render path never blocks on the network. It reads a cached snapshot and,
// when that is stale, spawns a detached refresh for the next frame. The OAuth
// token is never refreshed here; that is Claude Code's job, and racing it risks
// invalidating the credentials the editor is using.

const (
	usageURL        = "https://api.anthropic.com/api/oauth/usage"
	oauthBeta       = "oauth-2025-04-20"
	usageTimeout    = 6 * time.Second
	usageBodyLimit  = 1_000_000
	lockStale       = 30 * time.Second
	keychainTimeout = 3 * time.Second
	keychainService = "Claude Code-credentials"
)

// Bucket is one model-scoped weekly quota. JSON tags match the cache file the
// Node version wrote, so an existing ~/.cache/claude-readout/usage.json keeps
// working after the upgrade.
type Bucket struct {
	ID       string    `json:"id"`
	Label    string    `json:"label"`
	Percent  int       `json:"percent"`
	ResetsAt Timestamp `json:"resetsAt"`
	Severity *string   `json:"severity"`
	IsActive bool      `json:"isActive"`
}

// UsageCache is the on-disk snapshot. FetchedAt is epoch milliseconds.
type UsageCache struct {
	Scoped    []Bucket `json:"scoped"`
	FetchedAt int64    `json:"fetchedAt"`
	Error     string   `json:"error,omitempty"`
}

func (e Env) usageCachePath() string { return filepath.Join(e.cacheDir(), "usage.json") }
func (e Env) lockPath() string       { return filepath.Join(e.cacheDir(), "usage.lock") }

// readAccessToken reads Claude Code's OAuth access token: from
// <claudeConfigDir>/.credentials.json, else on macOS from the login Keychain.
// The Keychain service name is the one Claude Code itself uses, which it
// suffixes with the first 8 hex chars of the SHA-256 of the config dir when
// CLAUDE_CONFIG_DIR points somewhere other than $HOME/.claude. Both sources
// hold {"claudeAiOauth": {"accessToken", "expiresAt"}}; a bare {"accessToken"}
// or snake_case keys also work. An expired token reads as absent.
func readAccessToken(env Env, now time.Time) (string, bool) {
	credentials, err := os.ReadFile(filepath.Join(env.claudeConfigDir(), ".credentials.json"))
	if err == nil {
		if token, ok := accessTokenFromJSON(credentials, now); ok {
			return token, true
		}
	}
	if runtime.GOOS != "darwin" {
		return "", false
	}

	service := keychainService
	configDir := env.claudeConfigDir()
	if configDir != filepath.Join(env.Home, ".claude") {
		sum := sha256.Sum256([]byte(configDir))
		service += "-" + hex.EncodeToString(sum[:])[:8]
	}
	ctx, cancel := context.WithTimeout(context.Background(), keychainTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "security", "find-generic-password", "-s", service, "-w")
	cmd.Stderr = io.Discard
	raw, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return accessTokenFromJSON(raw, now)
}

func accessTokenFromJSON(data []byte, now time.Time) (string, bool) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil || root == nil {
		return "", false
	}
	oauth := root
	if raw, ok := root["claudeAiOauth"]; ok && !isJSONNull(raw) {
		var nested map[string]json.RawMessage
		if err := json.Unmarshal(raw, &nested); err != nil || nested == nil {
			return "", false
		}
		oauth = nested
	}

	tokenRaw, ok := oauth["accessToken"]
	if !ok || isJSONNull(tokenRaw) {
		tokenRaw, ok = oauth["access_token"]
	}
	if !ok {
		return "", false
	}
	var token string
	if err := json.Unmarshal(tokenRaw, &token); err != nil || token == "" {
		return "", false
	}

	expiresRaw, ok := oauth["expiresAt"]
	if !ok || isJSONNull(expiresRaw) {
		expiresRaw, ok = oauth["expires_at"]
	}
	if ok {
		var expires any
		if json.Unmarshal(expiresRaw, &expires) == nil {
			if value, numeric := expires.(float64); numeric && !math.IsNaN(value) && !math.IsInf(value, 0) && value <= float64(now.UnixMilli()) {
				return "", false
			}
		}
	}
	return token, true
}

func isJSONNull(raw json.RawMessage) bool {
	return strings.TrimSpace(string(raw)) == "null"
}

// parseScopedBuckets pulls kind == "weekly_scoped" entries out of the usage
// response's limits[] array. Percent is rounded and clamped to 0..100; an entry
// with a non-numeric percent or a blank scope.model.display_name is skipped.
// Entries are de-duplicated by lower-cased display name, an active entry
// replacing an inactive one. ID falls back to the lower-cased name. Order is
// first appearance.
func parseScopedBuckets(response []byte) []Bucket {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(response, &root); err != nil || root == nil {
		return []Bucket{}
	}
	var limits []json.RawMessage
	if err := json.Unmarshal(root["limits"], &limits); err != nil {
		return []Bucket{}
	}

	buckets := make([]Bucket, 0)
	positions := make(map[string]int)
	for _, raw := range limits {
		var entry map[string]json.RawMessage
		if err := json.Unmarshal(raw, &entry); err != nil || entry == nil {
			continue
		}
		var kind string
		if json.Unmarshal(entry["kind"], &kind) != nil || kind != "weekly_scoped" {
			continue
		}
		var percentValue any
		if json.Unmarshal(entry["percent"], &percentValue) != nil {
			continue
		}
		percentNumber, ok := percentValue.(float64)
		if !ok || math.IsNaN(percentNumber) || math.IsInf(percentNumber, 0) {
			continue
		}

		var scope map[string]json.RawMessage
		var model map[string]json.RawMessage
		if json.Unmarshal(entry["scope"], &scope) != nil || json.Unmarshal(scope["model"], &model) != nil {
			continue
		}
		var displayName string
		if json.Unmarshal(model["display_name"], &displayName) != nil || strings.TrimSpace(displayName) == "" {
			continue
		}
		label := strings.TrimSpace(displayName)
		key := strings.ToLower(label)
		id := ""
		if json.Unmarshal(model["id"], &id) != nil || id == "" {
			id = key
		}
		isActive := false
		_ = json.Unmarshal(entry["is_active"], &isActive)
		var severity *string
		var severityAny any
		if json.Unmarshal(entry["severity"], &severityAny) == nil {
			if severityValue, ok := severityAny.(string); ok {
				severity = &severityValue
			}
		}
		var resetsAt Timestamp
		_ = json.Unmarshal(entry["resets_at"], &resetsAt)
		bucket := Bucket{
			ID:       id,
			Label:    label,
			Percent:  int(math.Max(0, math.Min(100, jsRound(percentNumber)))),
			ResetsAt: resetsAt,
			Severity: severity,
			IsActive: isActive,
		}
		if position, exists := positions[key]; !exists {
			positions[key] = len(buckets)
			buckets = append(buckets, bucket)
		} else if isActive && !buckets[position].IsActive {
			buckets[position] = bucket
		}
	}
	return buckets
}

// readCachedUsage returns the snapshot, or nil when missing or malformed
// (a snapshot without a "scoped" array counts as malformed).
func readCachedUsage(env Env) *UsageCache {
	data, err := os.ReadFile(env.usageCachePath())
	if err != nil {
		return nil
	}
	var cached UsageCache
	if json.Unmarshal(data, &cached) != nil || cached.Scoped == nil {
		return nil
	}
	return &cached
}

// isStale is true for a nil or never-fetched snapshot, or one older than the TTL.
func isStale(cached *UsageCache, ttlSeconds float64, now time.Time) bool {
	if cached == nil || cached.FetchedAt == 0 {
		return true
	}
	return float64(now.UnixMilli()-cached.FetchedAt) > ttlSeconds*1000
}

// claimRefresh takes the single-writer lock (a directory, created with mkdir;
// one older than lockStale is removed and retaken). It returns false when
// another refresher holds it. The caller must hand ownership to a process that
// releases it, or call abandonRefresh.
func claimRefresh(env Env) bool {
	env.ensureCacheDir()
	path := env.lockPath()
	if err := os.Mkdir(path, 0o777); err == nil {
		return true
	}
	info, err := os.Stat(path)
	if err != nil || time.Since(info.ModTime()) <= lockStale {
		return false
	}
	if os.RemoveAll(path) != nil {
		return false
	}
	return os.Mkdir(path, 0o777) == nil
}

func abandonRefresh(env Env) {
	_ = os.RemoveAll(env.lockPath())
}

// fetchUsage GETs the usage endpoint with the bearer token. Redirects are not
// followed, so the token cannot be handed to another host.
func fetchUsage(token string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, usageURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", oauthBeta)
	req.Header.Set("Accept", "application/json")
	client := http.Client{
		Timeout: usageTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, usageBodyLimit+1))
	if err != nil {
		return nil, err
	}
	if len(body) > usageBodyLimit {
		return nil, fmt.Errorf("usage response too large")
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("usage endpoint returned %d", response.StatusCode)
	}
	return body, nil
}

// refreshUsage fetches and caches a snapshot. It runs in the detached child, so
// every failure is swallowed: a stale bar beats a broken statusline. On a
// failure after the token was found it keeps the previous buckets and moves
// FetchedAt to now, so the next attempt waits a full TTL instead of hammering
// a broken endpoint every frame.
func refreshUsage(env Env, lockHeld bool, fetch func(token string) ([]byte, error), now time.Time) *UsageCache {
	if !lockHeld && !claimRefresh(env) {
		return nil
	}
	defer abandonRefresh(env)

	writeCache := func(cache UsageCache) {
		env.ensureCacheDir()
		data, err := json.Marshal(cache)
		if err != nil {
			return
		}
		target := env.usageCachePath()
		tmp := fmt.Sprintf("%s.%d.tmp", target, os.Getpid())
		if err := os.WriteFile(tmp, data, 0o600); err != nil {
			return
		}
		_ = os.Rename(tmp, target)
	}

	token, ok := readAccessToken(env, now)
	if !ok {
		writeCache(UsageCache{Scoped: []Bucket{}, FetchedAt: now.UnixMilli(), Error: "no-token"})
		return nil
	}
	body, err := fetch(token)
	if err == nil && !json.Valid(body) {
		err = errors.New("usage response is not JSON")
	}
	if err != nil {
		previous := readCachedUsage(env)
		scoped := []Bucket{}
		if previous != nil {
			scoped = previous.Scoped
		}
		writeCache(UsageCache{Scoped: scoped, FetchedAt: now.UnixMilli(), Error: err.Error()})
		return nil
	}

	cache := UsageCache{Scoped: parseScopedBuckets(body), FetchedAt: now.UnixMilli()}
	writeCache(cache)
	return &cache
}
