/**
 * Config and XDG paths.
 *
 * The HUD works with no config file at all; `~/.config/claude-hud/config.json`
 * only ever overrides defaults. A malformed config must never take the
 * statusline down, so parse failures fall back to defaults silently (the
 * reason is available via `--doctor`).
 */

import { existsSync, mkdirSync, readFileSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";

import { DEFAULT_CONFIG } from "./theme.mjs";

export function configHome(env = process.env) {
  return env.XDG_CONFIG_HOME?.trim() || join(homedir(), ".config");
}

export function cacheHome(env = process.env) {
  return env.XDG_CACHE_HOME?.trim() || join(homedir(), ".cache");
}

export function configPath(env = process.env) {
  return env.CLAUDE_HUD_CONFIG?.trim() || join(configHome(env), "claude-hud", "config.json");
}

export function cacheDir(env = process.env) {
  return join(cacheHome(env), "claude-hud");
}

export function ensureCacheDir(env = process.env) {
  const dir = cacheDir(env);
  try {
    mkdirSync(dir, { recursive: true });
  } catch {
    /* a read-only cache dir degrades to "no cache", not to a broken statusline */
  }
  return dir;
}

/** Claude Code's own config dir, honouring CLAUDE_CONFIG_DIR. */
export function claudeConfigDir(env = process.env) {
  const configured = env.CLAUDE_CONFIG_DIR?.trim();
  if (!configured) return join(homedir(), ".claude");
  if (configured === "~") return homedir();
  if (configured.startsWith("~/")) return join(homedir(), configured.slice(2));
  return configured;
}

function isPlainObject(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

/** Deep merge for config objects; arrays and scalars from `override` win outright. */
function merge(base, override) {
  if (!isPlainObject(override)) return override === undefined ? base : override;
  const out = { ...base };
  for (const [key, value] of Object.entries(override)) {
    out[key] = isPlainObject(base?.[key]) ? merge(base[key], value) : value;
  }
  return out;
}

export function loadConfig(env = process.env) {
  const path = configPath(env);
  if (!existsSync(path)) return { config: DEFAULT_CONFIG, path, error: null };
  try {
    const parsed = JSON.parse(readFileSync(path, "utf8"));
    return { config: merge(DEFAULT_CONFIG, parsed), path, error: null };
  } catch (error) {
    return { config: DEFAULT_CONFIG, path, error: error?.message ?? String(error) };
  }
}
