/**
 * Line composition.
 *
 * Renders at most two lines: a context line (repo/branch/working tree) and the
 * meter line. Every element returns null when it has nothing to say, so the
 * separators never end up doubled or dangling.
 */

import { bold, dim, dimFg, fg, fgRgb, rampColor, stripAnsi } from "./color.mjs";
import { resolveGlyphs, resolvePalette } from "./theme.mjs";

const BAR_FILLED = "█"; // █
const BAR_EMPTY = "░"; // ░

/** Human duration from milliseconds: 45s, 20m, 1h5m, 3d2h. */
export function formatDuration(ms) {
  if (!Number.isFinite(ms) || ms < 0) return null;
  const totalMinutes = Math.floor(ms / 60_000);
  if (totalMinutes < 1) return `${Math.floor(ms / 1000)}s`;
  const days = Math.floor(totalMinutes / 1440);
  const hours = Math.floor((totalMinutes % 1440) / 60);
  const minutes = totalMinutes % 60;
  if (days > 0) return hours > 0 ? `${days}d${hours}h` : `${days}d`;
  if (hours > 0) return minutes > 0 ? `${hours}h${minutes}m` : `${hours}h`;
  return `${minutes}m`;
}

/** Time until an epoch-seconds or ISO timestamp, or null when already past. */
export function formatUntil(when) {
  if (when == null) return null;
  const target = typeof when === "number" ? when * 1000 : Date.parse(when);
  if (!Number.isFinite(target)) return null;
  const diff = target - Date.now();
  if (diff <= 0) return null;
  return formatDuration(diff);
}

/** Terminal cells a rendered string occupies, ignoring colour codes. */
export function visibleWidth(text) {
  let width = 0;
  for (const char of stripAnsi(text)) {
    const cp = char.codePointAt(0);
    if (cp === 0x200d || (cp >= 0x0300 && cp <= 0x036f)) continue; // ZWJ, combining
    // CJK, and emoji that terminals render double-wide.
    const wide =
      (cp >= 0x1100 && cp <= 0x115f) ||
      (cp >= 0x2e80 && cp <= 0xa4cf) ||
      (cp >= 0xac00 && cp <= 0xd7a3) ||
      (cp >= 0xf900 && cp <= 0xfaff) ||
      (cp >= 0xfe30 && cp <= 0xfe6f) ||
      (cp >= 0xff00 && cp <= 0xff60) ||
      (cp >= 0x1f300 && cp <= 0x1f9ff);
    width += wide ? 2 : 1;
  }
  return width;
}

function meter(percent, width, palette) {
  const safe = Number.isFinite(percent) ? Math.max(0, Math.min(100, percent)) : 0;
  const filled = Math.round((safe / 100) * width);
  const color = rampColor(safe, palette.bar);
  return (
    fgRgb(color, BAR_FILLED.repeat(filled)) + fg(palette.barEmpty, BAR_EMPTY.repeat(width - filled))
  );
}

/**
 * A labelled meter: `<glyph> 5h ████░░░░ 32% 4h5m`.
 *
 * `label` is what stops the line from being a row of anonymous bars. An icon
 * alone cannot say *which* quota a meter tracks — a clock and a calendar read
 * as "time" long before they read as "5-hour" and "weekly", and a model-scoped
 * bucket has no icon that could ever name the model. Meters that share a shape
 * therefore carry their name; the rest stay glyph-only.
 *
 * `tint` overrides the glyph colour (model-scoped buckets get their own hue).
 */
function gauge({ glyph, label, percent, resetsAt, width, palette, tint }) {
  if (percent == null) return null;
  const value = Math.round(percent);
  const parts = [];
  if (glyph) parts.push(fg(tint ?? palette.muted, glyph));
  if (label) parts.push(fg(tint ?? palette.muted, label));
  parts.push(meter(value, width, palette), fgRgb(rampColor(value, palette.bar), `${value}%`));
  // The reset time is context, not a headline: parenthesised and dimmed into the
  // muted tone so the eye lands on the percentage first.
  const until = formatUntil(resetsAt);
  if (until) parts.push(dimFg(palette.muted, `(${until})`));
  return parts.join(" ");
}

/**
 * Decide whether a meter shows its name.
 *
 * "auto" labels only the quota meters, which are the ones that sit side by side
 * looking alike. Context, session, thinking and tool count each have a glyph
 * that is unique on the line, so they stay bare.
 */
function labelFor({ mode, name, ambiguous }) {
  if (mode === "never") return null;
  if (mode === "always") return name;
  return ambiguous ? name : null;
}

function renderModel(payload, glyphs, palette) {
  const name = payload.model?.display_name ?? payload.model?.id;
  if (!name) return null;
  // "Opus 5 (1M context)" -> "Opus 5" plus a dim 1M marker, which keeps the
  // variant visible without spending a dozen cells on it.
  const short = String(name).replace(/\s*\(.*\)\s*$/, "").trim() || String(name);
  const large = (payload.context_window?.context_window_size ?? 0) >= 1_000_000;
  return `${fg(palette.muted, glyphs.model)} ${fg(palette.accent, short)}${large ? dim(" 1M") : ""}`;
}

function renderThinking(payload, glyphs, palette) {
  if (payload.thinking?.enabled !== true) return null;
  const effort = payload.effort?.level;
  return `${fg(palette.muted, glyphs.thinking)}${effort ? dim(` ${effort}`) : ""}`;
}

function renderSession(payload, glyphs, palette) {
  const duration = formatDuration(payload.cost?.total_duration_ms);
  if (!duration) return null;
  return `${fg(palette.muted, glyphs.session)} ${fg(palette.text, duration)}`;
}

function renderCost(payload, glyphs, palette) {
  const usd = payload.cost?.total_cost_usd;
  if (!Number.isFinite(usd)) return null;
  return `${fg(palette.muted, glyphs.cost)} ${fg(palette.text, `$${usd.toFixed(2)}`)}`;
}

function renderTools(count, glyphs, palette) {
  if (!Number.isFinite(count) || count <= 0) return null;
  return `${fg(palette.muted, glyphs.tools)} ${fg(palette.text, String(count))}`;
}

function renderGitLine(git, glyphs, palette, elements) {
  if (!git) return null;
  const parts = [];
  if (elements.repo && git.repo) {
    parts.push(`${fg(palette.muted, glyphs.repo)} ${fg(palette.text, git.repo)}`);
  }
  if (elements.branch && git.branch) {
    parts.push(`${fg(palette.muted, glyphs.branch)} ${fg(palette.accent, git.branch)}`);
  }
  if (elements.gitStatus) {
    const status = [];
    if (git.staged > 0) status.push(fg(palette.ok, `${glyphs.dirty}${git.staged}`));
    if (git.modified > 0) status.push(fg(palette.warn, `${glyphs.dirty}${git.modified}`));
    if (git.untracked > 0) status.push(fg(palette.muted, `${glyphs.untracked}${git.untracked}`));
    if (git.ahead > 0) status.push(fg(palette.ok, `${glyphs.ahead}${git.ahead}`));
    if (git.behind > 0) status.push(fg(palette.crit, `${glyphs.behind}${git.behind}`));
    if (status.length > 0) parts.push(status.join(" "));
  }
  return parts.length > 0 ? parts : null;
}

/**
 * @param {object} input
 * @param {object} input.payload  Claude Code statusline stdin, parsed.
 * @param {object} input.config
 * @param {object|null} input.git
 * @param {Array}  input.scoped   Model-scoped weekly buckets from the usage cache.
 * @param {number|null} input.toolCalls
 * @param {number|undefined} input.columns  Terminal width, when known.
 * @returns {string} the finished statusline (may contain one newline).
 */
export function renderHud({ payload, config, git, scoped, toolCalls, columns }) {
  const glyphs = resolveGlyphs(config);
  const palette = resolvePalette(config);
  const elements = config.elements ?? {};
  const sep = ` ${dim(config.separator ?? glyphs.separator)} `;
  const limits = payload.rate_limits ?? {};

  const main = [];
  const push = (value) => {
    if (value) main.push(value);
  };

  if (elements.model !== false) push(renderModel(payload, glyphs, palette));

  const labelMode = config.labels ?? "auto";

  if (elements.fiveHour !== false) {
    push(
      gauge({
        glyph: glyphs.fiveHour,
        label: labelFor({ mode: labelMode, name: config.names?.fiveHour ?? "5h", ambiguous: true }),
        percent: limits.five_hour?.used_percentage,
        resetsAt: limits.five_hour?.resets_at,
        width: config.barWidth,
        palette,
      }),
    );
  }

  if (elements.weekly !== false) {
    push(
      gauge({
        glyph: glyphs.weekly,
        label: labelFor({ mode: labelMode, name: config.names?.weekly ?? "wk", ambiguous: true }),
        percent: limits.seven_day?.used_percentage,
        resetsAt: limits.seven_day?.resets_at,
        width: config.barWidth,
        palette,
      }),
    );
  }

  if (elements.scoped !== false) {
    for (const bucket of scoped ?? []) {
      // A scoped bucket is named after a model, so it keeps its name even in
      // "never" mode — without it the bar is unidentifiable by construction.
      push(
        gauge({
          glyph: glyphs.scoped || "",
          label: bucket.label,
          percent: bucket.percent,
          resetsAt: bucket.resetsAt,
          width: config.barWidth,
          palette,
          tint: palette.scoped,
        }),
      );
    }
  }

  if (elements.context !== false) {
    const used = payload.context_window?.used_percentage;
    if (Number.isFinite(used)) {
      push(
        gauge({
          glyph: glyphs.context,
          label: labelFor({
            mode: labelMode,
            name: config.names?.context ?? "ctx",
            ambiguous: false,
          }),
          percent: used,
          width: config.contextBarWidth,
          palette,
        }),
      );
    }
  }

  if (elements.thinking !== false) push(renderThinking(payload, glyphs, palette));
  if (elements.session !== false) push(renderSession(payload, glyphs, palette));
  if (elements.tools !== false) push(renderTools(toolCalls, glyphs, palette));
  if (elements.cost === true) push(renderCost(payload, glyphs, palette));

  const lines = [];
  if (config.showGitLine !== false) {
    const gitParts = renderGitLine(git, glyphs, palette, elements);
    if (gitParts) lines.push(gitParts.join(sep));
  }

  // Drop trailing elements rather than letting the terminal hard-wrap mid-glyph.
  let mainLine = main.join(sep);
  if (Number.isFinite(columns) && columns > 20) {
    let kept = [...main];
    while (kept.length > 1 && visibleWidth(kept.join(sep)) > columns) kept.pop();
    mainLine = kept.join(sep);
  }
  lines.push(mainLine);

  return lines.join("\n");
}

export { bold };
