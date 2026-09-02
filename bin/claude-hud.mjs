#!/usr/bin/env node
/**
 * claude-hud — a Nerd Font statusline for Claude Code.
 *
 * Contract: read Claude Code's statusline JSON on stdin, print the line(s) on
 * stdout, exit 0. Anything that goes wrong is swallowed and the process still
 * prints something useful — a statusline that throws is a statusline that
 * blanks the pane.
 */

import { spawn } from "node:child_process";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { detectColorSupport, fg, fgRgb, rampColor, setColorEnabled } from "../src/color.mjs";
import { loadConfig } from "../src/config.mjs";
import { readGit } from "../src/git.mjs";
import { renderHud } from "../src/render.mjs";
import { countToolCalls } from "../src/transcript.mjs";
import {
  abandonRefresh,
  claimRefresh,
  isStale,
  readAccessToken,
  readCachedUsage,
  refreshUsage,
} from "../src/usage.mjs";
import { ELEMENT_MEANINGS, NERD_GLYPHS, resolvePalette } from "../src/theme.mjs";

const SELF = fileURLToPath(import.meta.url);

function readStdin() {
  try {
    return readFileSync(0, "utf8");
  } catch {
    return "";
  }
}

/**
 * Kick off a usage refresh for the *next* frame and return immediately. The
 * render path must never wait on the network.
 *
 * The lock is claimed here rather than in the child so that concurrent frames
 * during the child's startup do not each spawn a refresher of their own.
 */
function spawnRefresh() {
  if (!claimRefresh()) return; // someone else is already on it
  try {
    const child = spawn(process.execPath, [SELF, "--refresh-usage", "--lock-held"], {
      detached: true,
      stdio: "ignore",
    });
    child.unref();
  } catch {
    abandonRefresh(); // nothing will release it otherwise
  }
}

async function main() {
  const argv = process.argv.slice(2);

  if (argv.includes("--refresh-usage")) {
    await refreshUsage(process.env, { lockHeld: argv.includes("--lock-held") });
    return;
  }

  // A terminal statusline has no hover, so the legend is a command. It doubles
  // as a font test card: a box or blank means the glyph is missing.
  if (argv.includes("--legend") || argv.includes("--glyphs")) {
    console.log("Element legend — also a font check (boxes mean a missing glyph)\n");
    for (const [key, meaning] of Object.entries(ELEMENT_MEANINGS)) {
      const glyph = NERD_GLYPHS[key] ?? "";
      const cp = glyph.codePointAt(0)?.toString(16).toUpperCase().padStart(4, "0");
      const shown = glyph || "\u00b7"; // elements that render as text carry no icon
      console.log(`  ${shown}   ${key.padEnd(10)} ${(cp ? `U+${cp}` : "text").padEnd(7)} ${meaning}`);
    }
    console.log('\nOverride any glyph in config.json under "glyphs", or set "glyphs": "text"');
    console.log('for plain labels. "labels": "always" names every meter inline.');
    return;
  }

  const { config, path: cfgPath, error: cfgError } = loadConfig();

  // Palette tuning is guesswork without seeing the ramp, so print it.
  if (argv.includes("--ramp")) {
    setColorEnabled(detectColorSupport() && !argv.includes("--no-color"));
    const palette = resolvePalette(config);
    const width = config.barWidth ?? 8;
    console.log("Meter ramp at the configured palette:\n");
    for (let percent = 0; percent <= 100; percent += 5) {
      const rgb = rampColor(percent, palette.bar).map(Math.round);
      const hex = `#${rgb.map((c) => c.toString(16).padStart(2, "0")).join("")}`;
      const filled = Math.round((percent / 100) * width);
      const bar =
        fgRgb(rgb, "█".repeat(filled)) + fg(palette.barEmpty, "░".repeat(width - filled));
      console.log(
        `  ${String(percent).padStart(3)}%  ${bar}  ${fgRgb(rgb, hex)}${
          percent === 55 ? "   <- green holds to here" : ""
        }${percent === 80 ? "   <- amber" : ""}${percent === 100 ? "   <- red" : ""}`,
      );
    }
    console.log('\nTune it with "palette": { "bar": [ { "at": 0, "color": "#..." }, ... ] }');
    return;
  }

  if (argv.includes("--doctor")) {
    const cached = readCachedUsage();
    console.log(`config:      ${cfgPath}${cfgError ? `  (INVALID: ${cfgError})` : ""}`);
    console.log(`oauth token: ${readAccessToken() ? "found" : "not found (scoped buckets hidden)"}`);
    console.log(
      cached
        ? `usage cache: ${cached.scoped.length} scoped bucket(s), ${Math.round((Date.now() - cached.fetchedAt) / 1000)}s old`
        : "usage cache: empty (run once more, or --refresh-usage)",
    );
    console.log(`color:       ${detectColorSupport() ? "enabled" : "disabled"}`);
    return;
  }

  setColorEnabled(detectColorSupport() && !argv.includes("--no-color"));

  let payload = {};
  try {
    payload = JSON.parse(readStdin()) ?? {};
  } catch {
    payload = {};
  }

  const cwd = payload.workspace?.current_dir ?? payload.cwd ?? process.cwd();

  const cached = config.usageApi === false ? null : readCachedUsage();
  if (config.usageApi !== false && isStale(cached, config.usageTtlSeconds)) spawnRefresh();

  let git = null;
  if (config.showGitLine !== false) {
    try {
      git = readGit(cwd);
    } catch {
      git = null;
    }
  }

  let toolCalls = null;
  if (config.elements?.tools !== false) {
    try {
      toolCalls = countToolCalls(payload.transcript_path, payload.session_id);
    } catch {
      toolCalls = null;
    }
  }

  const columns = Number(process.env.CLAUDE_HUD_COLUMNS) || process.stdout.columns || undefined;

  console.log(
    renderHud({
      payload,
      config,
      git,
      scoped: cached?.scoped ?? [],
      toolCalls,
      columns,
    }),
  );
}

main().catch((error) => {
  // Last resort: say something rather than leave the pane empty.
  console.log(`claude-hud: ${error?.message ?? error}`);
  process.exitCode = 0;
});
