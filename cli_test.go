package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

var cliBinary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "claude-readout-cli-")
	if err != nil {
		panic(err)
	}
	cliBinary = filepath.Join(dir, "claude-readout")
	cmd := exec.Command("go", "build", "-o", cliBinary, ".")
	if out, buildErr := cmd.CombinedOutput(); buildErr != nil {
		os.Stderr.Write(out)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func runCLI(t *testing.T, input string, args ...string) string {
	t.Helper()
	return runCLIIn(t, t.TempDir(), input, args...)
}

func runCLIIn(t *testing.T, root string, input string, args ...string) string {
	t.Helper()
	fontDir := fontLocations(runtime.GOOS, root, func(key string) string {
		if key == "XDG_DATA_HOME" {
			return filepath.Join(root, "data")
		}
		return ""
	}).UserDir
	if err := os.MkdirAll(fontDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fontDir, "TestNerdFont-Regular.ttf"), []byte("font"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(cliBinary, args...)
	cmd.Stdin = strings.NewReader(input)
	cmd.Env = []string{
		"HOME=" + root,
		"XDG_CONFIG_HOME=" + filepath.Join(root, "config"),
		"XDG_CACHE_HOME=" + filepath.Join(root, "cache"),
		"XDG_DATA_HOME=" + filepath.Join(root, "data"),
		"CLAUDE_CONFIG_DIR=" + filepath.Join(root, "claude"),
		"NO_COLOR=1",
		"PATH=" + os.Getenv("PATH"),
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("claude-readout %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func TestCLIRendersFixture(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "fixture-session.json"))
	if err != nil {
		t.Fatal(err)
	}
	out := runCLI(t, string(fixture))
	if !strings.Contains(out, "Opus 5") || !regexp.MustCompile(`5h .*38%`).MatchString(out) || !strings.HasSuffix(out, "\n") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestCLIEmptyAndGarbageStillPrintALine(t *testing.T) {
	for _, input := range []string{"", "not json at all"} {
		out := runCLI(t, input)
		if out == "" || !strings.HasSuffix(out, "\n") {
			t.Fatalf("input %q produced %q", input, out)
		}
	}
}

func TestCLILegend(t *testing.T) {
	if out := runCLI(t, "", "--legend"); !strings.Contains(out, "weekly usage across all models") {
		t.Fatalf("legend output: %q", out)
	}
}

func TestCLIRamp(t *testing.T) {
	if out := runCLI(t, "", "--ramp"); !strings.Contains(out, "100%") {
		t.Fatalf("ramp output: %q", out)
	}
}

func TestCLIDoctor(t *testing.T) {
	if out := runCLI(t, "", "--doctor"); !strings.Contains(out, "theme:       dark\n") || !strings.Contains(out, "oauth token:") || !strings.Contains(out, "nerd font:   ") || strings.Contains(out, "nerd font:   not found") {
		t.Fatalf("doctor output: %q", out)
	}
}

func TestCLINoColorHasNoEscapeByte(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "fixture-session.json"))
	if err != nil {
		t.Fatal(err)
	}
	out := runCLI(t, string(fixture))
	if bytes.ContainsRune([]byte(out), '\x1b') {
		t.Fatalf("output contains escape byte: %q", out)
	}
}

// The 5-hour and weekly windows must survive a session that has not made an API
// call yet: Claude Code leaves rate_limits out of the payload until its first
// response, and the usage snapshot already holds both windows.
func TestCLIFillsRateWindowsFromUsageCacheBeforeTheFirstResponse(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(root, "cache", "claude-readout")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	snapshot := fmt.Sprintf(`{"scoped":[{"id":"fable","label":"Fable","percent":13,"resetsAt":null,"severity":"normal","isActive":false}],`+
		`"fiveHour":{"used_percentage":16,"resets_at":%d},"weekly":{"used_percentage":12,"resets_at":%d},"fetchedAt":%d}`,
		time.Now().Add(time.Hour).Unix(), time.Now().Add(48*time.Hour).Unix(), time.Now().UnixMilli())
	if err := os.WriteFile(filepath.Join(cacheDir, "usage.json"), []byte(snapshot), 0o600); err != nil {
		t.Fatal(err)
	}

	startup := `{"session_id":"s","model":{"id":"claude-opus-5[1m]","display_name":"Opus 5 (1M context)"},` +
		`"context_window":{"context_window_size":1000000},"thinking":{"enabled":true},"effort":{"level":"xhigh"}}`
	out := runCLIIn(t, root, startup)
	for _, pattern := range []string{`5h .*16%`, `wk .*12%`, `Fable .*13%`} {
		if !regexp.MustCompile(pattern).MatchString(out) {
			t.Errorf("%q did not match %q", out, pattern)
		}
	}
}
