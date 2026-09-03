package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
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
	root := t.TempDir()
	cmd := exec.Command(cliBinary, args...)
	cmd.Stdin = strings.NewReader(input)
	cmd.Env = []string{
		"HOME=" + root,
		"XDG_CONFIG_HOME=" + filepath.Join(root, "config"),
		"XDG_CACHE_HOME=" + filepath.Join(root, "cache"),
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
	if out := runCLI(t, "", "--doctor"); !strings.Contains(out, "oauth token:") {
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
