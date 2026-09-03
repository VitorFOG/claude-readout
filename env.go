package main

import (
	"os"
	"path/filepath"
	"strings"
)

// Env is the slice of the process environment the readout reads, captured once
// so tests can build one by hand instead of mutating the real environment.
type Env struct {
	Home            string
	XDGConfigHome   string // XDG_CONFIG_HOME
	XDGCacheHome    string // XDG_CACHE_HOME
	ReadoutConfig   string // READOUT_CONFIG: explicit config file path
	ClaudeConfigDir string // CLAUDE_CONFIG_DIR: Claude Code's own config dir
}

func envFromOS() Env {
	home, _ := os.UserHomeDir()
	return Env{
		Home:            home,
		XDGConfigHome:   os.Getenv("XDG_CONFIG_HOME"),
		XDGCacheHome:    os.Getenv("XDG_CACHE_HOME"),
		ReadoutConfig:   os.Getenv("READOUT_CONFIG"),
		ClaudeConfigDir: os.Getenv("CLAUDE_CONFIG_DIR"),
	}
}

// configHome is $XDG_CONFIG_HOME, or ~/.config when unset or blank.
func (e Env) configHome() string {
	if v := strings.TrimSpace(e.XDGConfigHome); v != "" {
		return v
	}
	return filepath.Join(e.Home, ".config")
}

// cacheHome is $XDG_CACHE_HOME, or ~/.cache when unset or blank.
func (e Env) cacheHome() string {
	if v := strings.TrimSpace(e.XDGCacheHome); v != "" {
		return v
	}
	return filepath.Join(e.Home, ".cache")
}

// configPath is $READOUT_CONFIG, or <configHome>/claude-readout/config.json.
func (e Env) configPath() string {
	if v := strings.TrimSpace(e.ReadoutConfig); v != "" {
		return v
	}
	return filepath.Join(e.configHome(), "claude-readout", "config.json")
}

func (e Env) cacheDir() string {
	return filepath.Join(e.cacheHome(), "claude-readout")
}

// ensureCacheDir creates the cache dir if it can. A read-only location degrades
// to "no cache", never to a broken statusline, so the error is dropped.
func (e Env) ensureCacheDir() string {
	dir := e.cacheDir()
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// claudeConfigDir is Claude Code's config dir: $CLAUDE_CONFIG_DIR with a
// leading "~" or "~/" expanded, else ~/.claude.
func (e Env) claudeConfigDir() string {
	configured := strings.TrimSpace(e.ClaudeConfigDir)
	switch {
	case configured == "":
		return filepath.Join(e.Home, ".claude")
	case configured == "~":
		return e.Home
	case strings.HasPrefix(configured, "~/"):
		return filepath.Join(e.Home, configured[2:])
	}
	return configured
}
