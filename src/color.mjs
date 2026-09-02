/**
 * Truecolor helpers.
 *
 * Every colour in the HUD goes through here so a theme can be swapped by
 * changing hex values alone. Falls back to plain text when the terminal (or
 * the user) says no colour.
 */

const ESC = "\x1b[";
export const RESET = `${ESC}0m`;

let enabled = true;

/** Disable colour globally (NO_COLOR, `--no-color`, dumb terminals). */
export function setColorEnabled(value) {
  enabled = Boolean(value);
}

export function colorEnabled() {
  return enabled;
}

/** Detect colour support from the environment, honouring the NO_COLOR standard. */
export function detectColorSupport(env = process.env) {
  if (env.NO_COLOR !== undefined && env.NO_COLOR !== "") return false;
  if (env.READOUT_NO_COLOR === "1") return false;
  const term = env.TERM ?? "";
  if (term === "dumb") return false;
  return true;
}

function clampByte(value) {
  return Math.max(0, Math.min(255, Math.round(value)));
}

/** Parse `#rrggbb` (or `#rgb`) into `[r, g, b]`. */
export function parseHex(hex) {
  const raw = String(hex).trim().replace(/^#/, "");
  const full = raw.length === 3 ? raw.replace(/(.)/g, "$1$1") : raw;
  if (!/^[0-9a-fA-F]{6}$/.test(full)) return [221, 221, 221];
  return [
    parseInt(full.slice(0, 2), 16),
    parseInt(full.slice(2, 4), 16),
    parseInt(full.slice(4, 6), 16),
  ];
}

/** Foreground colour from a hex string. */
export function fg(hex, text) {
  if (!enabled) return text;
  const [r, g, b] = parseHex(hex);
  return `${ESC}38;2;${r};${g};${b}m${text}${RESET}`;
}

/** Foreground colour from an `[r, g, b]` triple — used by the gradient. */
export function fgRgb(rgb, text) {
  if (!enabled) return text;
  const [r, g, b] = rgb;
  return `${ESC}38;2;${clampByte(r)};${clampByte(g)};${clampByte(b)}m${text}${RESET}`;
}

/**
 * Dim *and* coloured, in one sequence.
 *
 * `dim(fg(...))` looks right but is not: `fg` closes with RESET, which kills the
 * dim attribute mid-string. Both attributes have to be opened before the text
 * and closed once after it.
 */
export function dimFg(hex, text) {
  if (!enabled) return text;
  const [r, g, b] = parseHex(hex);
  return `${ESC}2m${ESC}38;2;${r};${g};${b}m${text}${RESET}`;
}

export function bold(text) {
  return enabled ? `${ESC}1m${text}${RESET}` : text;
}

export function dim(text) {
  return enabled ? `${ESC}2m${text}${RESET}` : text;
}

function lerp(a, b, t) {
  return a + (b - a) * t;
}

function mix(fromRgb, toRgb, t) {
  return [
    lerp(fromRgb[0], toRgb[0], t),
    lerp(fromRgb[1], toRgb[1], t),
    lerp(fromRgb[2], toRgb[2], t),
  ];
}

/**
 * Normalise a ramp definition into positioned stops.
 *
 * Accepts either `["#aaa", "#bbb"]` (spread evenly across 0-100) or
 * `[{ at: 60, color: "#aaa" }, ...]` when the ramp should hold a colour over a
 * range — which is what you want for a quota meter: comfortably green well
 * past the midpoint, then warming only as it approaches the cap.
 */
function normalizeStops(stops) {
  if (!Array.isArray(stops) || stops.length === 0) return [{ at: 0, rgb: [221, 221, 221] }];
  const positioned = stops.map((stop, index) => {
    if (typeof stop === "string") {
      return { at: (index / Math.max(1, stops.length - 1)) * 100, rgb: parseHex(stop) };
    }
    return {
      at: Number.isFinite(stop?.at) ? stop.at : (index / Math.max(1, stops.length - 1)) * 100,
      rgb: parseHex(stop?.color ?? stop?.hex ?? "#dddddd"),
    };
  });
  return positioned.sort((a, b) => a.at - b.at);
}

/**
 * Interpolate a percentage across the ramp.
 *
 * The stock Claude Code HUD snaps green -> yellow -> red at fixed thresholds,
 * so a bar at 69% and one at 71% look unrelated. A continuous ramp keeps
 * neighbouring values visually adjacent, which is the whole point of a meter.
 */
export function rampColor(percent, stops) {
  const p = Number.isFinite(percent) ? Math.max(0, Math.min(100, percent)) : 0;
  const points = normalizeStops(stops);
  if (points.length === 1) return points[0].rgb;
  if (p <= points[0].at) return points[0].rgb;
  const last = points[points.length - 1];
  if (p >= last.at) return last.rgb;
  for (let i = 0; i < points.length - 1; i += 1) {
    const from = points[i];
    const to = points[i + 1];
    if (p >= from.at && p <= to.at) {
      const span = to.at - from.at;
      return span === 0 ? to.rgb : mix(from.rgb, to.rgb, (p - from.at) / span);
    }
  }
  return last.rgb;
}

/** Visible width of a string, ignoring ANSI SGR sequences. */
export function stripAnsi(text) {
  return String(text).replace(/\x1b\[[0-9;]*m/g, "");
}
