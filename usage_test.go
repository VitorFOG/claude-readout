package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func usageTestEnv(t *testing.T) Env {
	t.Helper()
	root := t.TempDir()
	return Env{
		Home:            root,
		XDGCacheHome:    filepath.Join(root, "cache"),
		ClaudeConfigDir: filepath.Join(root, "claude"),
	}
}

func writeUsageToken(t *testing.T, env Env, token string) {
	t.Helper()
	if err := os.MkdirAll(env.claudeConfigDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"claudeAiOauth":{"accessToken":"` + token + `"}}`)
	if err := os.WriteFile(filepath.Join(env.claudeConfigDir(), ".credentials.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readUsageJSON(t *testing.T, env Env) map[string]any {
	t.Helper()
	data, err := os.ReadFile(env.usageCachePath())
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestParseScopedBucketsExtractsWeeklyEntries(t *testing.T) {
	response := []byte(`{"limits":[
		{"kind":"session","percent":36,"resets_at":"2026-09-02T22:00:00Z","scope":null},
		{"kind":"weekly_all","percent":14,"resets_at":"2026-09-05T20:00:00Z","scope":null},
		{"kind":"weekly_scoped","percent":22,"severity":"normal","resets_at":"2026-09-05T20:00:00Z","scope":{"model":{"id":null,"display_name":"Fable"}},"is_active":false}
	]}`)

	buckets := parseScopedBuckets(response)
	if len(buckets) != 1 {
		t.Fatalf("got %d buckets, want 1", len(buckets))
	}
	got := buckets[0]
	if got.ID != "fable" || got.Label != "Fable" || got.Percent != 22 || got.Severity == nil || *got.Severity != "normal" {
		t.Fatalf("unexpected bucket: %#v", got)
	}
	if string(got.ResetsAt.Raw) != `"2026-09-05T20:00:00Z"` {
		t.Fatalf("resetsAt raw = %s", got.ResetsAt.Raw)
	}
}

func TestParseScopedBucketsPrefersActiveDuplicateInOriginalPosition(t *testing.T) {
	buckets := parseScopedBuckets([]byte(`{"limits":[
		{"kind":"weekly_scoped","percent":10,"scope":{"model":{"display_name":"Fable"}},"is_active":false},
		{"kind":"weekly_scoped","percent":30,"scope":{"model":{"display_name":"Other"}},"is_active":false},
		{"kind":"weekly_scoped","percent":55,"scope":{"model":{"display_name":"fable"}},"is_active":true}
	]}`))
	if len(buckets) != 2 || buckets[0].Label != "fable" || buckets[0].Percent != 55 || !buckets[0].IsActive || buckets[1].Label != "Other" {
		t.Fatalf("unexpected buckets: %#v", buckets)
	}
}

func TestParseScopedBucketsSkipsMalformedAndClampsPercent(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"absent limits", `{}`},
		{"non-array limits", `{"limits":"nope"}`},
		{"invalid json", `{`},
		{"bad entries", `{"limits":[null,{"kind":"weekly_scoped","percent":"5","scope":{"model":{"display_name":"A"}}},{"kind":"weekly_scoped","percent":5,"scope":{"model":{"display_name":"  "}}}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseScopedBuckets([]byte(tt.body)); len(got) != 0 {
				t.Fatalf("got %#v, want no buckets", got)
			}
		})
	}

	buckets := parseScopedBuckets([]byte(`{"limits":[
		{"kind":"weekly_scoped","percent":140,"scope":{"model":{"id":"","display_name":" Over "}}},
		{"kind":"weekly_scoped","percent":-2.5,"scope":{"model":{"display_name":"Under"}},"severity":4}
	]}`))
	if len(buckets) != 2 || buckets[0].ID != "over" || buckets[0].Percent != 100 || buckets[1].Percent != 0 || buckets[1].Severity != nil {
		t.Fatalf("unexpected buckets: %#v", buckets)
	}
}

func TestRefreshUsageSuccessWritesBuckets(t *testing.T) {
	env := usageTestEnv(t)
	writeUsageToken(t, env, "token")
	now := time.UnixMilli(1_788_000_000_123)
	body := []byte(`{"limits":[{"kind":"weekly_scoped","percent":38,"resets_at":1788638400,"scope":{"model":{"id":"opus","display_name":"Opus"}},"is_active":true}]}`)

	got := refreshUsage(env, false, func(token string) ([]byte, error) {
		if token != "token" {
			t.Fatalf("token = %q", token)
		}
		return body, nil
	}, now)
	if got == nil || len(got.Scoped) != 1 || got.Scoped[0].Label != "Opus" {
		t.Fatalf("refresh result = %#v", got)
	}

	cache := readUsageJSON(t, env)
	if cache["fetchedAt"] != float64(now.UnixMilli()) || cache["error"] != nil {
		t.Fatalf("unexpected cache metadata: %#v", cache)
	}
	scoped, ok := cache["scoped"].([]any)
	if !ok || len(scoped) != 1 || scoped[0].(map[string]any)["label"] != "Opus" {
		t.Fatalf("unexpected scoped cache: %#v", cache["scoped"])
	}
	if _, err := os.Stat(env.lockPath()); !os.IsNotExist(err) {
		t.Fatalf("refresh lock remains: %v", err)
	}
}

func TestRefreshUsageTreatsValidExtremeJSONNumberAsSuccess(t *testing.T) {
	env := usageTestEnv(t)
	writeUsageToken(t, env, "token")
	if err := os.MkdirAll(env.cacheDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	previous := `{"scoped":[{"id":"old","label":"Old","percent":12,"resetsAt":null,"severity":null,"isActive":false}],"fetchedAt":1}`
	if err := os.WriteFile(env.usageCachePath(), []byte(previous), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.UnixMilli(1_788_000_000_321)
	body := []byte(`{"limits":[{"kind":"weekly_scoped","percent":1e1000,"scope":{"model":{"display_name":"Huge"}}}]}`)

	got := refreshUsage(env, false, func(string) ([]byte, error) { return body, nil }, now)
	if got == nil || len(got.Scoped) != 0 {
		t.Fatalf("refresh result = %#v, want successful empty snapshot", got)
	}
	cache := readUsageJSON(t, env)
	if _, hasError := cache["error"]; hasError || len(cache["scoped"].([]any)) != 0 {
		t.Fatalf("unexpected cache: %#v", cache)
	}
}

func TestRefreshUsageErrorKeepsPreviousBuckets(t *testing.T) {
	env := usageTestEnv(t)
	writeUsageToken(t, env, "token")
	if err := os.MkdirAll(env.cacheDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	previous := `{"scoped":[{"id":"fable","label":"Fable","percent":12,"resetsAt":null,"severity":null,"isActive":false}],"fetchedAt":1}`
	if err := os.WriteFile(env.usageCachePath(), []byte(previous), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.UnixMilli(1_788_000_000_456)

	if got := refreshUsage(env, false, func(string) ([]byte, error) {
		return nil, errors.New("offline")
	}, now); got != nil {
		t.Fatalf("refresh result = %#v, want nil", got)
	}

	cache := readUsageJSON(t, env)
	if cache["fetchedAt"] != float64(now.UnixMilli()) || cache["error"] != "offline" {
		t.Fatalf("unexpected cache metadata: %#v", cache)
	}
	scoped := cache["scoped"].([]any)
	if len(scoped) != 1 || scoped[0].(map[string]any)["label"] != "Fable" {
		t.Fatalf("previous buckets not retained: %#v", scoped)
	}
}

func TestRefreshUsageWithoutTokenWritesNoToken(t *testing.T) {
	env := usageTestEnv(t)
	now := time.UnixMilli(1_788_000_000_789)
	called := false

	if got := refreshUsage(env, false, func(string) ([]byte, error) {
		called = true
		return nil, nil
	}, now); got != nil {
		t.Fatalf("refresh result = %#v, want nil", got)
	}
	if called {
		t.Fatal("fetch called without a token")
	}
	cache := readUsageJSON(t, env)
	if cache["error"] != "no-token" || cache["fetchedAt"] != float64(now.UnixMilli()) || len(cache["scoped"].([]any)) != 0 {
		t.Fatalf("unexpected cache: %#v", cache)
	}
}

func TestClaimRefreshRejectsSecondClaim(t *testing.T) {
	env := usageTestEnv(t)
	if !claimRefresh(env) {
		t.Fatal("first claim failed")
	}
	t.Cleanup(func() { abandonRefresh(env) })
	if claimRefresh(env) {
		t.Fatal("second claim succeeded")
	}
}

func TestClaimRefreshRetakesStaleLock(t *testing.T) {
	env := usageTestEnv(t)
	if !claimRefresh(env) {
		t.Fatal("initial claim failed")
	}
	old := time.Now().Add(-lockStale - time.Second)
	if err := os.Chtimes(env.lockPath(), old, old); err != nil {
		t.Fatal(err)
	}
	if !claimRefresh(env) {
		t.Fatal("stale lock was not retaken")
	}
	abandonRefresh(env)
}
