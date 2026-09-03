package main

import (
	"context"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const gitTimeout = 400 * time.Millisecond

var (
	aheadPattern       = regexp.MustCompile(`ahead (\d+)`)
	behindPattern      = regexp.MustCompile(`behind (\d+)`)
	branchSplitPattern = regexp.MustCompile(`\.\.\.|\s+\[`)
)

// GitInfo is what the context line shows about the working tree.
type GitInfo struct {
	Repo      string // basename of the top-level dir, "" when unknown
	Branch    string // "detached" on a detached HEAD
	Staged    int
	Modified  int
	Untracked int
	Ahead     int
	Behind    int
}

// readGit reads the working tree state from one porcelain status call, or nil
// when cwd is not a work tree, git is missing, or git is slower than gitTimeout
// (a statusline cannot wait on a huge repo). The repo name costs a second git
// process, so it is only read when wantRepo is set.
//
// Header shapes: "## main...origin/main [ahead 2, behind 1]", "## HEAD (no
// branch)" (Branch becomes "detached"), "## No commits yet on main" (the prefix
// is dropped). The branch is the header up to "..." or " [". Status lines: "??"
// is untracked; otherwise a non-space, non-"?" X column counts as staged and a
// non-space, non-"?" Y column as modified.
func readGit(cwd string, wantRepo bool) *GitInfo {
	run := func(args ...string) (string, bool) {
		ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = cwd
		cmd.Stderr = io.Discard
		out, err := cmd.Output()
		if err != nil {
			return "", false
		}
		return string(out), true
	}

	raw, ok := run("status", "--porcelain=v1", "-b", "--untracked-files=normal")
	if !ok {
		return nil
	}
	result := &GitInfo{}
	for _, line := range strings.Split(raw, "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "## ") {
			header := strings.TrimPrefix(line[3:], "No commits yet on ")
			branch := branchSplitPattern.Split(header, 2)[0]
			if strings.HasPrefix(branch, "HEAD (no branch)") {
				result.Branch = "detached"
			} else {
				result.Branch = branch
			}
			if match := aheadPattern.FindStringSubmatch(header); match != nil {
				result.Ahead, _ = strconv.Atoi(match[1])
			}
			if match := behindPattern.FindStringSubmatch(header); match != nil {
				result.Behind, _ = strconv.Atoi(match[1])
			}
			continue
		}
		if strings.HasPrefix(line, "??") {
			result.Untracked++
			continue
		}
		if line[0] != ' ' && line[0] != '?' {
			result.Staged++
		}
		if len(line) > 1 && line[1] != ' ' && line[1] != '?' {
			result.Modified++
		}
	}

	if !wantRepo {
		return result
	}
	if top, ok := run("rev-parse", "--show-toplevel"); ok {
		top = strings.TrimSpace(top)
		result.Repo = top[strings.LastIndex(top, "/")+1:]
	}
	return result
}
