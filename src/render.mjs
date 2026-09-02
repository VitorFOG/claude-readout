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
  if (!(width > 0)) return null; // width 0 = numbers only, the tightest layout
  const safe = Number.isFinite(percent) ? Math.max(0, Math.min(100, percent)) : 0;
  // Any non-zero usage shows at least one block. On a 3-cell bar 14% rounds to
  // zero filled, and an empty bar states something false — that none is used.
  const scaled = Math.round((safe / 100) * width);
  const filled = safe > 0 ? Math.max(1, scaled) : scaled;
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
function gauge({ glyph, label, percent, resetsAt, width, palette, tint, showReset = true }) {
  if (percent == null) return null;
  const value = Math.round(percent);
  const parts = [];
  if (glyph) parts.push(fg(tint ?? palette.muted, glyph));
  // Under `"glyphs": "text"` the glyph already IS the name, so printing the
  // label too would render "5h 5h 38%".
  if (label && label !== glyph) parts.push(fg(tint ?? palette.muted, label));
  const bar = meter(value, width, palette);
  if (bar) parts.push(bar);
  parts.push(fgRgb(rampColor(value, palette.bar), `${value}%`));
  // The reset time is context, not a headline: parenthesised and dimmed into the
  // muted tone so the eye lands on the percentage first.
  const until = showReset ? formatUntil(resetsAt) : null;
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
 * Progressive compaction levels, widest first.
 *
 * Claude Code hard-truncates an over-long statusline with an ellipsis, which
 * silently eats whole elements off the right. Shrinking on our own terms keeps
 * every element visible for far longer: reset times go first (they are the
 * least urgent thing on the line), then the bars narrow, and finally the bars
 * disappear entirely and the meters read as bare percentages.
 */
function compactionLevels(config) {
  const bar = config.barWidth ?? 8;
  const ctx = config.contextBarWidth ?? 10;
  return [
    { showResets: true, bar, ctx },
    { showResets: false, bar, ctx },
    { showResets: false, bar: Math.min(bar, 5), ctx: Math.min(ctx, 6) },
    { showResets: false, bar: 3, ctx: 3 },
    { showResets: false, bar: 0, ctx: 0 },
  ];
}

/**
 * Order in which elements are given up when even the tightest layout does not
 * fit. Least useful first: a tool count is trivia next to how much of your
 * weekly quota is gone.
 */
const DEFAULT_SHED_ORDER = ["cost", "tools", "session", "thinking", "context", "scoped", "weekly", "fiveHour", "model"];

/** Build the main-line elements as re-renderable descriptors. */
function buildElements({ payload, config, scoped, toolCalls, glyphs, palette }) {
  const elements = config.elements ?? {};
  const limits = payload.rate_limits ?? {};
  const labelMode = config.labels ?? "auto";
  const out = [];
  const add = (key, render) => out.push({ key, render });

  if (elements.model !== false) add("model", () => renderModel(payload, glyphs, palette));

  if (elements.fiveHour !== false) {
    add("fiveHour", (level) =>
      gauge({
        glyph: glyphs.fiveHour,
        label: labelFor({ mode: labelMode, name: config.names?.fiveHour ?? "5h", ambiguous: true }),
        percent: limits.five_hour?.used_percentage,
        resetsAt: limits.five_hour?.resets_at,
        width: level.bar,
        showReset: level.showResets,
        palette,
      }));
  }

  if (elements.weekly !== false) {
    add("weekly", (level) =>
      gauge({
        glyph: glyphs.weekly,
        label: labelFor({ mode: labelMode, name: config.names?.weekly ?? "wk", ambiguous: true }),
        percent: limits.seven_day?.used_percentage,
        resetsAt: limits.seven_day?.resets_at,
        width: level.bar,
        showReset: level.showResets,
        palette,
      }));
  }

  if (elements.scoped !== false) {
    for (const bucket of scoped ?? []) {
      // A scoped bucket is named after a model, so it keeps its name at every
      // level — without it the bar is unidentifiable by construction.
      add("scoped", (level) =>
        gauge({
          glyph: glyphs.scoped || "",
          label: bucket.label,
          percent: bucket.percent,
          resetsAt: bucket.resetsAt,
          width: level.bar,
          showReset: level.showResets,
          palette,
          tint: palette.scoped,
        }));
    }
  }

  if (elements.context !== false) {
    const used = payload.context_window?.used_percentage;
    if (Number.isFinite(used)) {
      add("context", (level) =>
        gauge({
          glyph: glyphs.context,
          label: labelFor({ mode: labelMode, name: config.names?.context ?? "ctx", ambiguous: false }),
          percent: used,
          width: level.ctx,
          palette,
        }));
    }
  }

  if (elements.thinking !== false) add("thinking", () => renderThinking(payload, glyphs, palette));
  if (elements.session !== false) add("session", () => renderSession(payload, glyphs, palette));
  if (elements.tools !== false) add("tools", () => renderTools(toolCalls, glyphs, palette));
  if (elements.cost === true) add("cost", () => renderCost(payload, glyphs, palette));

  return out;
}

/** Split a rendered list across as many lines as the width requires. */
function wrapParts(parts, sep, width) {
  const lines = [];
  let current = [];
  for (const part of parts) {
    const candidate = [...current, part];
    if (current.length > 0 && visibleWidth(candidate.join(sep)) > width) {
      lines.push(current.join(sep));
      current = [part];
    } else {
      current = candidate;
    }
  }
  if (current.length > 0) lines.push(current.join(sep));
  return lines;
}

/** Cut a string to `width` cells, marking the cut with an ellipsis. */
function clip(text, width) {
  if (visibleWidth(text) <= width) return text;
  const plain = stripAnsi(text);
  return `${plain.slice(0, Math.max(0, width - 1))}\u2026`;
}

/**
 * @param {object} input
 * @param {object} input.payload  Claude Code statusline stdin, parsed.
 * @param {object} input.config
 * @param {object|null} input.git
 * @param {Array}  input.scoped   Model-scoped weekly buckets from the usage cache.
 * @param {number|null} input.toolCalls
 * @param {number|undefined} input.columns  Terminal width, when known.
 * @returns {string} the finished statusline (may contain newlines).
 */
export function renderHud({ payload, config, git, scoped, toolCalls, columns }) {
  const glyphs = resolveGlyphs(config);
  const palette = resolvePalette(config);
  const sep = ` ${dim(config.separator ?? glyphs.separator)} `;
  const overflow = config.overflow ?? "shrink";

  const descriptors = buildElements({ payload, config, scoped, toolCalls, glyphs, palette });
  const levels = compactionLevels(config);
  const compose = (level, keep) =>
    descriptors
      .filter((d) => !keep || keep.has(d))
      .map((d) => d.render(level))
      .filter(Boolean);

  const width =
    Number.isFinite(columns) && columns > 20
      ? Math.max(20, columns - (config.reserveColumns ?? 2))
      : null;

  let parts = compose(levels[0]);
  if (width !== null && overflow !== "none") {
    // Take the widest layout that fits.
    const level = levels.find((l) => visibleWidth(compose(l).join(sep)) <= width);
    if (level) {
      parts = compose(level);
    } else if (overflow === "wrap") {
      parts = compose(levels[levels.length - 1]);
    } else {
      // Still too wide at the tightest layout: give up elements, least useful
      // first, rather than letting the terminal amputate the right-hand end.
      const tightest = levels[levels.length - 1];
      const shedOrder = config.shedOrder ?? DEFAULT_SHED_ORDER;
      const keep = new Set(descriptors);
      for (const key of shedOrder) {
        if (visibleWidth(compose(tightest, keep).join(sep)) <= width) break;
        for (const d of [...keep].reverse()) {
          if (d.key === key) {
            keep.delete(d);
            break; // shed one instance at a time; scoped can repeat
          }
        }
      }
      parts = compose(tightest, keep);
    }
  }

  const lines = [];
  if (config.showGitLine !== false) {
    const gitParts = renderGitLine(git, glyphs, palette, config.elements ?? {});
    if (gitParts) {
      const gitLine = gitParts.join(sep);
      lines.push(width === null ? gitLine : clip(gitLine, width));
    }
  }

  if (width !== null && overflow === "wrap") {
    lines.push(...wrapParts(parts, sep, width));
  } else {
    lines.push(parts.join(sep));
  }

  return lines.join("\n");
}

export { bold };
