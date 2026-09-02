/**
 * Git facts for the context line.
 *
 * One `git status --porcelain=v1 -b` call covers branch, upstream divergence
 * and working-tree counts, so the statusline pays for a single process per
 * render instead of three.
 */

import { execFileSync } from "node:child_process";

const GIT_TIMEOUT_MS = 400;

function git(args, cwd) {
  return execFileSync("git", args, {
    cwd,
    encoding: "utf8",
    timeout: GIT_TIMEOUT_MS,
    stdio: ["ignore", "pipe", "ignore"],
    maxBuffer: 2_000_000,
  });
}

/**
 * @returns {{repo: string|null, branch: string|null, staged: number,
 *   modified: number, untracked: number, ahead: number, behind: number}|null}
 *   or null when `cwd` is not inside a work tree.
 */
export function readGit(cwd) {
  let raw;
  try {
    raw = git(["status", "--porcelain=v1", "-b", "--untracked-files=normal"], cwd);
  } catch {
    return null; // not a repo, git missing, or slower than the timeout
  }

  const result = {
    repo: null,
    branch: null,
    staged: 0,
    modified: 0,
    untracked: 0,
    ahead: 0,
    behind: 0,
  };

  for (const line of raw.split("\n")) {
    if (line === "") continue;
    if (line.startsWith("## ")) {
      // Header shapes: "main...origin/main [ahead 2]", "HEAD (no branch)",
      // and "No commits yet on main" for a freshly initialised repo.
      const header = line.slice(3).replace(/^No commits yet on /, "");
      const branchPart = header.split(/\.\.\.|\s+\[/)[0];
      result.branch = branchPart.startsWith("HEAD (no branch)") ? "detached" : branchPart;
      const ahead = header.match(/ahead (\d+)/);
      const behind = header.match(/behind (\d+)/);
      if (ahead) result.ahead = Number(ahead[1]);
      if (behind) result.behind = Number(behind[1]);
      continue;
    }
    if (line.startsWith("??")) {
      result.untracked += 1;
      continue;
    }
    // XY status: X is the index (staged), Y the work tree (unstaged).
    if (line[0] !== " " && line[0] !== "?") result.staged += 1;
    if (line[1] !== " " && line[1] !== "?") result.modified += 1;
  }

  try {
    const top = git(["rev-parse", "--show-toplevel"], cwd).trim();
    if (top) result.repo = top.split("/").pop() ?? null;
  } catch {
    /* branch alone is enough for the line */
  }

  return result;
}
