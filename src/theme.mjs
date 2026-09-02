/**
 * Theme: glyphs, palette and layout defaults.
 *
 * Every glyph and colour is overridable from the user config, so a terminal
 * without a Nerd Font (or someone who just dislikes robots) can swap them
 * without touching code. `text` is the fallback set — plain ASCII labels.
 */

export const NERD_GLYPHS = {
  model: "\u{F06A9}", // md-robot
  fiveHour: "\u{F017}", // fa-clock
  weekly: "\u{F073}", // fa-calendar
  // Model-scoped buckets print the model's own name, so an icon would only
  // repeat it. Empty by default; set one here to get a glyph back.
  scoped: "",
  context: "\u{F035B}", // md-memory
  session: "\u{F051F}", // md-timer-sand
  tools: "\u{F0AD}", // fa-wrench
  thinking: "\u{F09D1}", // md-brain
  branch: "\u{E725}", // dev-git-branch
  repo: "\u{F401}", // oct-repo
  cost: "\u{F0BC5}", // md-cash
  dirty: "\u{F06D5}", // md-pencil
  untracked: "\u{F02D6}", // md-help
  ahead: "\u{2191}",
  behind: "\u{2193}",
  separator: "\u{2502}", // box drawing vertical
};

/**
 * What each element means, in one line. Printed by `--legend`, which is the
 * closest a terminal statusline gets to a tooltip.
 */
export const ELEMENT_MEANINGS = {
  model: "active model (1M marks the long-context variant)",
  fiveHour: "5-hour usage window, with time until it resets",
  weekly: "weekly usage across all models, with time until it resets",
  scoped: "weekly quota for one model — shown as its name (Fable, ...), no icon",
  context: "context window used in this session",
  thinking: "extended thinking, with the effort level",
  session: "how long this session has been running",
  tools: "tool calls made in this session",
  branch: "current git branch",
  repo: "repository name (off by default)",
  cost: "session spend in USD (off by default)",
  dirty: "staged / modified files",
  untracked: "untracked files",
  ahead: "commits ahead of upstream",
  behind: "commits behind upstream",
  separator: "divider between elements",
};

export const TEXT_GLYPHS = {
  model: "model",
  fiveHour: "5h",
  weekly: "wk",
  scoped: "",
  context: "ctx",
  session: "up",
  tools: "tools",
  thinking: "think",
  branch: "branch",
  repo: "repo",
  cost: "$",
  dirty: "!",
  untracked: "?",
  ahead: "^",
  behind: "v",
  separator: "|",
};

/**
 * Tokyo Night derived palette. Tuned for a dark background — the meter ramp
 * runs green -> amber -> red so a bar's colour tracks its value continuously
 * rather than snapping at fixed thresholds.
 */
export const DEFAULT_PALETTE = {
  accent: "#7aa2f7", // model, branch
  muted: "#565f89", // labels, separators, parenthetical detail
  text: "#c0caf5",
  /**
   * Meter ramp. Holds green through the range where usage is simply fine, then
   * warms toward amber and red as the cap approaches — an evenly spread ramp
   * would paint a comfortable 40% the same colour as a worrying 60%.
   */
  bar: [
    { at: 0, color: "#9ece6a" },
    { at: 55, color: "#9ece6a" },
    { at: 80, color: "#e0af68" },
    { at: 100, color: "#f7768e" },
  ],
  barEmpty: "#3b4261",
  scoped: "#bb9af7", // model-scoped buckets (Fable and friends)
  ok: "#9ece6a",
  warn: "#e0af68",
  crit: "#f7768e",
};

export const DEFAULT_CONFIG = {
  glyphs: "nerd", // "nerd" | "text" | object of overrides
  palette: {},
  barWidth: 8,
  contextBarWidth: 10,
  /**
   * Meter naming: "auto" names the quota meters (which look alike side by
   * side), "always" names every meter, "never" leaves them bare. Model-scoped
   * buckets always show their model name — nothing else identifies them.
   */
  labels: "auto",
  names: { fiveHour: "5h", weekly: "wk", context: "ctx" },
  separator: null, // null = use the theme glyph
  /**
   * What to do when the line will not fit: "shrink" compacts, then gives up the
   * least useful elements; "wrap" compacts, then continues onto more lines;
   * "none" renders full width and lets Claude Code truncate.
   */
  overflow: "shrink",
  /** Cells held back from COLUMNS for the pane's own border and padding. */
  reserveColumns: 2,
  showGitLine: true,
  elements: {
    model: true,
    fiveHour: true,
    weekly: true,
    scoped: true, // per-model weekly buckets (Fable, ...)
    context: true,
    session: true,
    thinking: true,
    tools: true,
    cost: false,
    repo: false,
    branch: true,
    gitStatus: true,
  },
  /** Seconds before the cached usage snapshot is refreshed in the background. */
  usageTtlSeconds: 120,
  /** Skip the usage API entirely; 5h and weekly still come from Claude Code. */
  usageApi: true,
};

export function resolveGlyphs(config) {
  const mode = config.glyphs ?? "nerd";
  if (mode === "text") return { ...TEXT_GLYPHS };
  if (mode && typeof mode === "object") return { ...NERD_GLYPHS, ...mode };
  return { ...NERD_GLYPHS };
}

export function resolvePalette(config) {
  return { ...DEFAULT_PALETTE, ...(config.palette ?? {}) };
}
