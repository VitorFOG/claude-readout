package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func transcriptTestEnv(t *testing.T) Env {
	t.Helper()
	return Env{XDGCacheHome: filepath.Join(t.TempDir(), "cache")}
}

func TestTranscriptCountsIncrementalAppendsWithoutDoubleCounting(t *testing.T) {
	env := transcriptTestEnv(t)
	path := filepath.Join(t.TempDir(), "session.jsonl")
	toolLine := `{"content":[{"type":"tool_use","name":"Bash"}]}`
	plainLine := `{"content":[{"type":"text","text":"hi"}]}`
	if err := os.WriteFile(path, []byte(toolLine+"\n"+plainLine+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, ok := countToolCalls(path, "s1", env); !ok || got != 1 {
		t.Fatalf("first count = %d, %v", got, ok)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(toolLine + "\n" + toolLine + "\n"); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if got, ok := countToolCalls(path, "s1", env); !ok || got != 3 {
		t.Fatalf("incremental count = %d, %v", got, ok)
	}
	if got, ok := countToolCalls(path, "s1", env); !ok || got != 3 {
		t.Fatalf("stable count = %d, %v", got, ok)
	}
}

func TestTranscriptIgnoresPartialTrailingLine(t *testing.T) {
	env := transcriptTestEnv(t)
	path := filepath.Join(t.TempDir(), "partial.jsonl")
	toolLine := `{"content":[{"type":"tool_use"}]}`
	if err := os.WriteFile(path, []byte(toolLine+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, ok := countToolCalls(path, "s2", env); !ok || got != 1 {
		t.Fatalf("first count = %d, %v", got, ok)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"content":[{"type":"tool_u`); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if got, ok := countToolCalls(path, "s2", env); !ok || got != 1 {
		t.Fatalf("partial count = %d, %v", got, ok)
	}
	f, err = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`se"}]}` + "\n"); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if got, ok := countToolCalls(path, "s2", env); !ok || got != 2 {
		t.Fatalf("completed count = %d, %v", got, ok)
	}
}

func TestTranscriptHandlesUTF8ByteOffsets(t *testing.T) {
	env := transcriptTestEnv(t)
	path := filepath.Join(t.TempDir(), "utf8.jsonl")
	wide := `{"text":"日本語テキスト — em dash","content":[{"type":"tool_use"}]}`
	if err := os.WriteFile(path, []byte(wide+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, ok := countToolCalls(path, "s3", env); !ok || got != 1 {
		t.Fatalf("first count = %d, %v", got, ok)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(wide + "\n"); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if got, ok := countToolCalls(path, "s3", env); !ok || got != 2 {
		t.Fatalf("second count = %d, %v", got, ok)
	}
}

func TestTranscriptMissingIsNotCounted(t *testing.T) {
	env := transcriptTestEnv(t)
	if got, ok := countToolCalls(filepath.Join(t.TempDir(), "missing.jsonl"), "s4", env); ok || got != 0 {
		t.Fatalf("missing count = %d, %v", got, ok)
	}
	if got, ok := countToolCalls("", "s5", env); ok || got != 0 {
		t.Fatalf("empty path count = %d, %v", got, ok)
	}
}

func TestTranscriptNegativeCursorOffsetDoesNotPanic(t *testing.T) {
	env := transcriptTestEnv(t)
	path := filepath.Join(t.TempDir(), "negative-cursor.jsonl")
	line := `{"content":[{"type":"tool_use"}]}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(env.cacheDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.cacheDir(), "tools-negative.json"), []byte(`{"offset":-1,"count":0,"size":0}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if got, ok := countToolCalls(path, "negative", env); !ok || got != 1 {
		t.Fatalf("count = %d, %v", got, ok)
	}
}

func TestTranscriptShortLinesAreNotDoubleCounted(t *testing.T) {
	env := transcriptTestEnv(t)
	path := filepath.Join(t.TempDir(), "short.jsonl")
	line := `{"type":"tool_use"}` + "\n"
	if err := os.WriteFile(path, []byte(strings.Repeat(line, 3)), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, ok := countToolCalls(path, "short", env); !ok || got != 3 {
		t.Fatalf("first count = %d, %v", got, ok)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(line); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if got, ok := countToolCalls(path, "short", env); !ok || got != 4 {
		t.Fatalf("count after one short append = %d, %v (want 4)", got, ok)
	}
}
