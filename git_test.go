package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func runGitTestCommand(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func TestReadGitCountsWorkingTreeAndBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	repo := filepath.Join(t.TempDir(), "sample-repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitTestCommand(t, repo, "init", "-b", "main")
	for name, body := range map[string]string{"staged.txt": "one\n", "modified.txt": "one\n"} {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runGitTestCommand(t, repo, "add", ".")
	runGitTestCommand(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "initial")
	if err := os.WriteFile(filepath.Join(repo, "staged.txt"), []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitTestCommand(t, repo, "add", "staged.txt")
	if err := os.WriteFile(filepath.Join(repo, "modified.txt"), []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "untracked.txt"), []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := readGit(repo, true)
	if got == nil {
		t.Fatal("readGit returned nil")
	}
	if got.Repo != "sample-repo" || got.Branch != "main" || got.Staged != 1 || got.Modified != 1 || got.Untracked != 1 || got.Ahead != 0 || got.Behind != 0 {
		t.Fatalf("unexpected git info: %#v", got)
	}
}

func TestReadGitDetachedHead(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	repo := filepath.Join(t.TempDir(), "detached-repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitTestCommand(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitTestCommand(t, repo, "add", ".")
	runGitTestCommand(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "initial")
	runGitTestCommand(t, repo, "checkout", "--detach", "HEAD")

	got := readGit(repo, true)
	if got == nil || got.Branch != "detached" {
		t.Fatalf("unexpected git info: %#v", got)
	}
}

func TestReadGitOutsideRepositoryReturnsNil(t *testing.T) {
	if got := readGit(t.TempDir(), true); got != nil {
		t.Fatalf("readGit = %#v, want nil", got)
	}
}
