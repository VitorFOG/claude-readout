package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// runSetup points Claude Code's statusline at this binary by writing
//
//	"statusLine": {"type": "command", "command": "<absolute path of this binary>"}
//
// into <claudeConfigDir>/settings.json. Every other key in the file, and every
// other key inside statusLine, is carried over byte for byte: the file belongs
// to Claude Code and holds hook commands and ids this tool must not reformat.
// The absolute path matters: Claude Code runs the statusline through a
// non-interactive shell whose PATH lacks nvm, fnm and friends, and going
// through npm's JS launcher would put a Node startup in front of every frame.
//
// A settings file that is not a JSON object is left alone and the snippet is
// printed for the user to apply by hand. Nothing here exits non-zero.
func runSetup(w io.Writer, env Env) {
	self := executablePath()
	if self == "" {
		fmt.Fprintln(w, "claude-readout: cannot find my own path")
		return
	}
	command := shellQuote(self)
	path := filepath.Join(env.claudeConfigDir(), "settings.json")
	if target, err := filepath.EvalSymlinks(path); err == nil {
		path = target // a dotfiles-managed symlink is updated in place, not replaced by a plain file
	}

	settings, mode, err := readSettings(path)
	if err != nil {
		snippet, _ := encodeJSON(map[string]any{"statusLine": map[string]any{"type": "command", "command": command}}, "  ")
		fmt.Fprintf(w, "claude-readout: not touching %s (%v)\nAdd this to it by hand:\n%s", path, err, snippet)
		return
	}

	var statusLine map[string]json.RawMessage
	if raw, ok := settings["statusLine"]; ok {
		_ = json.Unmarshal(raw, &statusLine)
	}
	if statusLine == nil {
		statusLine = map[string]json.RawMessage{}
	}
	var currentType, currentCommand string
	_ = json.Unmarshal(statusLine["type"], &currentType)
	_ = json.Unmarshal(statusLine["command"], &currentCommand)
	if currentType == "command" && currentCommand == command {
		fmt.Fprintf(w, "claude-readout: %s already points at %s\n", path, command)
		return
	}
	statusLine["type"], _ = encodeJSON("command", "")
	statusLine["command"], _ = encodeJSON(command, "")
	settings["statusLine"], _ = encodeJSON(statusLine, "")

	if err := writeSettings(path, settings, mode); err != nil {
		fmt.Fprintf(w, "claude-readout: could not write %s (%v)\n", path, err)
		return
	}
	fmt.Fprintf(w, "claude-readout: statusLine in %s now runs %s\nRestart Claude Code to see it.\n", path, command)
}

// shellQuote makes the path safe for the shell Claude Code hands the
// statusLine command to. A path that needs no quoting is written bare, which
// is the form --doctor prints and the README shows.
func shellQuote(path string) string {
	if runtime.GOOS == "windows" {
		if strings.ContainsAny(path, " \t&|<>^()") {
			return `"` + path + `"`
		}
		return path
	}
	if !strings.ContainsAny(path, " \t\n'\"\\$`&|;<>()[]{}*?#~!") {
		return path
	}
	return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
}

// encodeJSON is json.Marshal without HTML escaping, so "&&" in a hook command
// or a "<" in an env value come back out as written. Indented output ends with
// a newline, compact output does not.
func encodeJSON(value any, indent string) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", indent)
	if err := enc.Encode(value); err != nil {
		return nil, err
	}
	if indent == "" {
		return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
	}
	return buf.Bytes(), nil
}

// readSettings returns the file's top-level keys with their values as raw
// JSON, an empty map when the file is missing or blank, and an error when it
// exists but is not a JSON object. Values stay raw so numbers above 2^53 and
// strings with shell characters survive the round trip untouched.
func readSettings(path string) (map[string]json.RawMessage, os.FileMode, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]json.RawMessage{}, 0o600, nil
	}
	if err != nil {
		return nil, 0, err
	}
	mode := info.Mode().Perm()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]json.RawMessage{}, mode, nil
	}
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(data, &settings); err != nil {
		var syntax *json.SyntaxError
		if errors.As(err, &syntax) {
			return nil, 0, fmt.Errorf("not valid JSON: %w", err)
		}
		return nil, 0, errors.New("top level is not an object")
	}
	if settings == nil {
		return nil, 0, errors.New("top level is not an object")
	}
	return settings, mode, nil
}

// writeSettings writes the object with two-space indentation through a rename,
// so a crash mid-write cannot leave Claude Code with a truncated settings file.
// The previous file mode is kept.
func writeSettings(path string, settings map[string]json.RawMessage, mode os.FileMode) error {
	data, err := encodeJSON(settings, "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
