/**
 * End-to-end checks against the real entry points.
 *
 * The unit tests exercise `renderHud` directly, which cannot catch a launcher
 * pointing at the wrong file or a flag that throws before rendering — and both
 * of those show up as an empty statusline, the one failure mode a statusline
 * must never have.
 */

import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { after, describe, it } from "node:test";

const here = dirname(fileURLToPath(import.meta.url));
const root = join(here, "..");
const entry = join(root, "bin", "claude-readout.mjs");
const launcher = join(root, "bin", "claude-readout.sh");
const fixture = readFileSync(join(here, "fixture-session.json"), "utf8");

// An isolated HOME so the suite never reads or writes the developer's own
// config, cache or credentials.
const sandbox = mkdtempSync(join(tmpdir(), "claude-readout-cli-"));
after(() => rmSync(sandbox, { recursive: true, force: true }));

const env = {
  PATH: process.env.PATH,
  HOME: sandbox,
  XDG_CONFIG_HOME: join(sandbox, "config"),
  XDG_CACHE_HOME: join(sandbox, "cache"),
  CLAUDE_CONFIG_DIR: join(sandbox, "claude"), // no credentials -> no usage call
  NO_COLOR: "1",
};

function run(command, args, input = "") {
  return execFileSync(command, args, { input, env, encoding: "utf8", timeout: 15_000 });
}

describe("cli", () => {
  it("renders from a statusline payload", () => {
    const out = run(process.execPath, [entry], fixture);
    assert.match(out, /Opus 5/);
    assert.match(out, /5h .*38%/);
    assert.equal(out.endsWith("\n"), true);
  });

  it("still prints a line when stdin is empty or garbage", () => {
    assert.equal(typeof run(process.execPath, [entry], ""), "string");
    assert.equal(typeof run(process.execPath, [entry], "not json at all"), "string");
  });

  it("runs through the shell launcher", () => {
    // Regression: the launcher hard-codes its entry filename, so a rename can
    // silently point it at a file that no longer exists.
    const out = run("sh", [launcher], fixture);
    assert.match(out, /Opus 5/);
  });

  it("finds node even with no PATH inherited", () => {
    const out = execFileSync("sh", [launcher], {
      input: fixture,
      env: { HOME: sandbox, NO_COLOR: "1" },
      encoding: "utf8",
      timeout: 15_000,
    });
    assert.match(out, /Opus 5/);
  });

  it("prints the legend, the ramp and the doctor report", () => {
    assert.match(run(process.execPath, [entry, "--legend"]), /weekly usage across all models/);
    assert.match(run(process.execPath, [entry, "--ramp"]), /100%/);
    assert.match(run(process.execPath, [entry, "--doctor"]), /oauth token:/);
  });

  it("honours NO_COLOR", () => {
    const out = run(process.execPath, [entry], fixture);
    assert.equal(/\x1b\[/.test(out), false);
  });
});
