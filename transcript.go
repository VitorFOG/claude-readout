package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var toolUseMarker = regexp.MustCompile(`"type"\s*:\s*"tool_use"`)

type transcriptCursor struct {
	Offset int64 `json:"offset"`
	Count  int   `json:"count"`
	Size   int64 `json:"size"`
}

// countToolCalls counts `"type": "tool_use"` markers in the session transcript
// (a JSONL file that grows to megabytes) by reading only the bytes appended
// since the last render, with a per-session cursor in the cache dir. A cursor
// that does not fit the current file (size shrank, offset negative or past the
// end) is dropped, which is how a rotated transcript restarts from zero. Only
// whole lines count and the cursor is left just past the last newline read, so
// the next frame resumes on a line boundary and the trailing partial line
// waits. All arithmetic stays in bytes: transcripts contain multi-byte UTF-8.
func countToolCalls(transcriptPath, sessionID string, env Env) (int, bool) {
	if transcriptPath == "" {
		return 0, false
	}
	info, err := os.Stat(transcriptPath)
	if err != nil {
		return 0, false
	}
	size := info.Size()
	if sessionID == "" {
		sessionID = transcriptPath
	}
	var safe strings.Builder
	for _, r := range sessionID {
		if r < 128 && ((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-') {
			safe.WriteRune(r)
		} else {
			safe.WriteByte('_')
		}
	}
	cursorPath := filepath.Join(env.cacheDir(), "tools-"+safe.String()+".json")

	var offset int64
	var count int
	var cached struct {
		Offset *int64 `json:"offset"`
		Count  *int   `json:"count"`
		Size   *int64 `json:"size"`
	}
	if data, err := os.ReadFile(cursorPath); err == nil && json.Unmarshal(data, &cached) == nil &&
		cached.Offset != nil && cached.Count != nil && cached.Size != nil &&
		*cached.Size <= size && *cached.Offset >= 0 && *cached.Offset <= size {
		offset = *cached.Offset
		count = *cached.Count
	}
	if offset >= size {
		return count, true
	}

	file, err := os.Open(transcriptPath)
	if err != nil {
		if count > 0 {
			return count, true
		}
		return 0, false
	}
	defer file.Close()
	buffer := make([]byte, size-offset)
	read, readErr := file.ReadAt(buffer, offset)
	if readErr != nil && readErr != io.EOF {
		if count > 0 {
			return count, true
		}
		return 0, false
	}
	view := buffer[:read]
	lastNewline := bytes.LastIndexByte(view, '\n')
	if lastNewline == -1 {
		return count, true
	}
	count += len(toolUseMarker.FindAll(view[:lastNewline+1], -1))
	offset += int64(lastNewline) + 1

	cursor := transcriptCursor{Offset: offset, Count: count, Size: size}
	if data, marshalErr := json.Marshal(cursor); marshalErr == nil {
		env.ensureCacheDir()
		tmp := cursorPath + "." + strconv.Itoa(os.Getpid()) + ".tmp"
		if writeErr := os.WriteFile(tmp, data, 0o666); writeErr == nil {
			_ = os.Rename(tmp, cursorPath)
		}
	}
	return count, true
}
