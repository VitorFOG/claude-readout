package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSetupWritesStatusLineAndKeepsEverythingElse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	before := `{"theme":"dark","permissions":{"allow":["Bash"]},` +
		`"hooks":{"Stop":[{"command":"grep -q '<x>' f && echo ok > out"}]},` +
		`"someBigId":12345678901234567890,"ratio":1.10,` +
		`"statusLine":{"type":"command","command":"/old/shim","padding":0}}`
	if err := os.WriteFile(path, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	runSetupForSettingsTest(t, &out, Env{Home: dir, ClaudeConfigDir: dir})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("settings.json is no longer JSON: %v\n%s", err, data)
	}
	if settings["theme"] != "dark" {
		t.Errorf("theme key lost: %v", settings)
	}
	if _, ok := settings["permissions"].(map[string]any); !ok {
		t.Errorf("permissions key lost: %v", settings)
	}
	for _, literal := range []string{`"grep -q '<x>' f && echo ok > out"`, `12345678901234567890`, `1.10`, `"padding": 0`} {
		if !strings.Contains(string(data), literal) {
			t.Errorf("%s did not survive the rewrite:\n%s", literal, data)
		}
	}
	self, _ := os.Executable()
	want := shellQuote(self)
	statusLine, _ := settings["statusLine"].(map[string]any)
	if statusLine["type"] != "command" || statusLine["command"] != want {
		t.Errorf("statusLine = %v, want command %s", statusLine, want)
	}
	if !strings.Contains(out.String(), "now runs "+want) {
		t.Errorf("unexpected report: %s", out.String())
	}

	out.Reset()
	runSetupForSettingsTest(t, &out, Env{Home: dir, ClaudeConfigDir: dir})
	if !strings.Contains(out.String(), "already points at") {
		t.Errorf("second run should be a no-op, got: %s", out.String())
	}
}

func TestSetupKeepsFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no POSIX modes")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runSetupForSettingsTest(t, &bytes.Buffer{}, Env{Home: dir, ClaudeConfigDir: dir})
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("mode = %o, want 644", info.Mode().Perm())
	}
}

func TestSetupCreatesMissingSettings(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	runSetupForSettingsTest(t, &out, Env{Home: dir, ClaudeConfigDir: filepath.Join(dir, "claude")})
	data, err := os.ReadFile(filepath.Join(dir, "claude", "settings.json"))
	if err != nil {
		t.Fatalf("settings.json not created: %v", err)
	}
	if !strings.Contains(string(data), `"statusLine"`) {
		t.Errorf("no statusLine in %s", data)
	}
}

func TestSetupRefusesMalformedSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	for name, content := range map[string]string{"syntax": `{"theme": `, "array": `[1,2]`} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		runSetupForSettingsTest(t, &out, Env{Home: dir, ClaudeConfigDir: dir})
		after, _ := os.ReadFile(path)
		if string(after) != content {
			t.Errorf("%s: malformed file was modified: %s", name, after)
		}
		if !strings.Contains(out.String(), "not touching") || !strings.Contains(out.String(), `"statusLine"`) {
			t.Errorf("%s: expected a refusal with a snippet, got: %s", name, out.String())
		}
	}
}

func TestShellQuote(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX quoting")
	}
	for path, want := range map[string]string{
		"/usr/local/bin/claude-readout": "/usr/local/bin/claude-readout",
		"/Users/Jo Smith/bin/readout":   "'/Users/Jo Smith/bin/readout'",
		"/home/o'brien/bin/readout":     `'/home/o'\''brien/bin/readout'`,
		"/opt/a&b/readout":              "'/opt/a&b/readout'",
		"/opt/readout[old]/readout":     "'/opt/readout[old]/readout'",
		"/home/me/.nvm/v22/bin/readout": "/home/me/.nvm/v22/bin/readout",
	} {
		if got := shellQuote(path); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestSetupFollowsSymlinkedSettings(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "dotfiles", "settings.json")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(`{"theme":"dark"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "settings.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	runSetupForSettingsTest(t, &bytes.Buffer{}, Env{Home: dir, ClaudeConfigDir: dir})
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("settings.json is no longer a symlink: %v %v", info, err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"statusLine"`) || !strings.Contains(string(data), `"theme": "dark"`) {
		t.Errorf("symlink target not updated in place: %s", data)
	}
}

func TestSetupInstallsMissingNerdFontThenFindsIt(t *testing.T) {
	t.Setenv("PATH", "")
	root := t.TempDir()
	fontDir := filepath.Join(root, "fonts")
	locations := FontLocations{UserDir: fontDir, Dirs: []string{fontDir}, CanInstall: true}
	archive := zipFixture(t, []zipEntry{{name: "SymbolsNerdFontMono-Regular.ttf", contents: "font"}})
	fetchCalls := 0
	fetch := func(url string) (io.ReadCloser, error) {
		fetchCalls++
		return io.NopCloser(bytes.NewReader(archive)), nil
	}
	var out bytes.Buffer

	runSetup(&out, Env{Home: root, ClaudeConfigDir: filepath.Join(root, "claude")}, locations, fetch)

	installed := filepath.Join(fontDir, "SymbolsNerdFontMono-Regular.ttf")
	if fetchCalls != 1 {
		t.Fatalf("fetch calls = %d, want 1", fetchCalls)
	}
	if data, err := os.ReadFile(installed); err != nil || string(data) != "font" {
		t.Fatalf("installed font = %q, %v, want font contents", data, err)
	}
	for _, want := range []string{
		"claude-readout: no Nerd Font in " + fontDir,
		"claude-readout: installing Symbols Nerd Font Mono into " + fontDir,
		"claude-readout: installed " + installed,
		"Restart your terminal if the glyphs still show as boxes; claude-readout --legend prints them all.",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("setup output missing %q:\n%s", want, out.String())
		}
	}

	out.Reset()
	runSetup(&out, Env{Home: root, ClaudeConfigDir: filepath.Join(root, "claude")}, locations, func(string) (io.ReadCloser, error) {
		t.Fatal("fetch called when a Nerd Font was already installed")
		return nil, errors.New("unreachable")
	})
	if !strings.Contains(out.String(), "claude-readout: nerd font: "+installed) {
		t.Fatalf("second setup did not find installed font:\n%s", out.String())
	}
}

func TestSetupMalformedSettingsStillRunsFontStep(t *testing.T) {
	t.Setenv("PATH", "")
	root := t.TempDir()
	settings := filepath.Join(root, "settings.json")
	if err := os.WriteFile(settings, []byte(`{"theme": `), 0o600); err != nil {
		t.Fatal(err)
	}
	fontDir := filepath.Join(root, "fonts")
	archive := zipFixture(t, []zipEntry{{name: "SymbolsNerdFontMono-Regular.ttf", contents: "font"}})
	var out bytes.Buffer

	runSetup(
		&out,
		Env{Home: root, ClaudeConfigDir: root},
		FontLocations{UserDir: fontDir, Dirs: []string{fontDir}, CanInstall: true},
		func(string) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(archive)), nil },
	)

	if !strings.Contains(out.String(), "not touching") || !strings.Contains(out.String(), "claude-readout: installed ") {
		t.Fatalf("setup did not report both malformed settings and font installation:\n%s", out.String())
	}
	if _, err := os.Stat(filepath.Join(fontDir, "SymbolsNerdFontMono-Regular.ttf")); err != nil {
		t.Fatalf("font step did not run: %v", err)
	}
}

func runSetupForSettingsTest(t *testing.T, w io.Writer, env Env) {
	t.Helper()
	fontDir := t.TempDir()
	font := filepath.Join(fontDir, "TestNerdFont-Regular.ttf")
	if err := os.WriteFile(font, []byte("font"), 0o644); err != nil {
		t.Fatal(err)
	}
	runSetup(w, env, FontLocations{UserDir: fontDir, Dirs: []string{fontDir}, CanInstall: true}, func(string) (io.ReadCloser, error) {
		t.Fatal("fetch called with a Nerd Font present")
		return nil, errors.New("unreachable")
	})
}
