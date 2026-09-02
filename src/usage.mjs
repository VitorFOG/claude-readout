/**
 * Usage snapshot for model-scoped weekly buckets.
 *
 * Claude Code's statusline stdin already carries the 5-hour and weekly windows,
 * so this module exists for the one thing stdin does NOT expose: per-model
 * weekly quotas (`kind: "weekly_scoped"`, e.g. Fable). It reads them from
 * `GET api.anthropic.com/api/oauth/usage` using the OAuth token Claude Code
 * already stores.
 *
 * Design rules:
 *  - The render path never blocks on the network. It reads a cached snapshot
 *    and, when that snapshot is stale, spawns a detached refresh for the *next*
 *    frame. A cold cache costs the scoped buckets one frame, nothing else.
 *  - We never refresh the OAuth token ourselves. That is Claude Code's job, and
 *    racing it risks invalidating the credentials the editor is using. An
 *    expired token simply means no scoped buckets until Claude Code renews it.
 */

import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { existsSync, readFileSync, renameSync, writeFileSync, mkdirSync, statSync, rmSync } from "node:fs";
import https from "node:https";
import { join } from "node:path";

import { cacheDir, claudeConfigDir, ensureCacheDir } from "./config.mjs";

const USAGE_HOST = "api.anthropic.com";
const USAGE_PATH = "/api/oauth/usage";
const OAUTH_BETA = "oauth-2025-04-20";
const REQUEST_TIMEOUT_MS = 6000;
const LOCK_STALE_MS = 30_000;

export function usageCachePath(env = process.env) {
  return join(cacheDir(env), "usage.json");
}

function lockPath(env = process.env) {
  return join(cacheDir(env), "usage.lock");
}

/**
 * Read the OAuth access token.
 *
 * Linux/WSL keep it in `~/.claude/.credentials.json`; macOS keeps it in the
 * login Keychain under a config-dir-scoped account name.
 */
export function readAccessToken(env = process.env) {
  const filePath = join(claudeConfigDir(env), ".credentials.json");
  if (existsSync(filePath)) {
    const token = tokenFromJson(safeReadJson(filePath));
    if (token) return token;
  }
  if (process.platform === "darwin") {
    const token = tokenFromJson(readKeychainCredentials(env));
    if (token) return token;
  }
  return null;
}

function safeReadJson(path) {
  try {
    return JSON.parse(readFileSync(path, "utf8"));
  } catch {
    return null;
  }
}

function tokenFromJson(json) {
  if (!json || typeof json !== "object") return null;
  const oauth = json.claudeAiOauth ?? json;
  const token = oauth?.accessToken ?? oauth?.access_token;
  if (typeof token !== "string" || token === "") return null;
  const expiresAt = oauth?.expiresAt ?? oauth?.expires_at;
  if (typeof expiresAt === "number" && Number.isFinite(expiresAt) && expiresAt <= Date.now()) {
    return null; // expired — Claude Code will renew it; we must not.
  }
  return token;
}

function readKeychainCredentials(env) {
  // Claude Code scopes the Keychain item by config dir when it is non-default.
  const configDir = claudeConfigDir(env);
  const defaultDir = join(env.HOME ?? "", ".claude");
  const service =
    configDir === defaultDir
      ? "Claude Code-credentials"
      : `Claude Code-credentials-${createHash("sha256").update(configDir).digest("hex").slice(0, 8)}`;
  try {
    const raw = execFileSync("security", ["find-generic-password", "-s", service, "-w"], {
      encoding: "utf8",
      stdio: ["ignore", "pipe", "ignore"],
      timeout: 3000,
    });
    return JSON.parse(raw);
  } catch {
    return null;
  }
}

/** One HTTPS GET against the usage endpoint. Resolves to the parsed body. */
function fetchUsage(token) {
  return new Promise((resolve, reject) => {
    const req = https.request(
      {
        hostname: USAGE_HOST,
        path: USAGE_PATH,
        method: "GET",
        headers: {
          Authorization: `Bearer ${token}`,
          "anthropic-beta": OAUTH_BETA,
          Accept: "application/json",
        },
        timeout: REQUEST_TIMEOUT_MS,
      },
      (res) => {
        let body = "";
        res.setEncoding("utf8");
        res.on("data", (chunk) => {
          body += chunk;
          if (body.length > 1_000_000) req.destroy(new Error("usage response too large"));
        });
        res.on("end", () => {
          if (res.statusCode !== 200) {
            reject(new Error(`usage endpoint returned ${res.statusCode}`));
            return;
          }
          try {
            resolve(JSON.parse(body));
          } catch (error) {
            reject(error);
          }
        });
      },
    );
    req.on("timeout", () => req.destroy(new Error("usage request timed out")));
    req.on("error", reject);
    req.end();
  });
}

function clampPercent(value) {
  if (!Number.isFinite(value)) return null;
  return Math.max(0, Math.min(100, Math.round(value)));
}

/**
 * Pull model-scoped weekly buckets out of the response.
 *
 * The `limits[]` array is the forward-compatible shape: a new model tier shows
 * up as another `weekly_scoped` entry with its own `scope.model.display_name`,
 * so it renders without a release here. Entries are de-duplicated by display
 * name, preferring the active one.
 */
export function parseScopedBuckets(response) {
  const limits = response?.limits;
  if (!Array.isArray(limits)) return [];
  const byName = new Map();
  for (const entry of limits) {
    if (!entry || typeof entry !== "object" || entry.kind !== "weekly_scoped") continue;
    const percent = clampPercent(entry.percent);
    if (percent === null) continue;
    const name = entry.scope?.model?.display_name;
    if (typeof name !== "string" || name.trim() === "") continue;
    const label = name.trim();
    const key = label.toLowerCase();
    const isActive = entry.is_active === true;
    if (!byName.has(key) || (isActive && !byName.get(key).isActive)) {
      byName.set(key, {
        id: entry.scope?.model?.id ?? key,
        label,
        percent,
        resetsAt: entry.resets_at ?? null,
        severity: typeof entry.severity === "string" ? entry.severity : null,
        isActive,
      });
    }
  }
  return [...byName.values()];
}

export function parseUsage(response) {
  return {
    scoped: parseScopedBuckets(response),
    fetchedAt: Date.now(),
  };
}

export function readCachedUsage(env = process.env) {
  const cached = safeReadJson(usageCachePath(env));
  if (!cached || typeof cached !== "object" || !Array.isArray(cached.scoped)) return null;
  return cached;
}

export function isStale(cached, ttlSeconds) {
  if (!cached?.fetchedAt) return true;
  return Date.now() - cached.fetchedAt > ttlSeconds * 1000;
}

function writeCache(payload, env = process.env) {
  ensureCacheDir(env);
  const target = usageCachePath(env);
  const tmp = `${target}.${process.pid}.tmp`;
  writeFileSync(tmp, JSON.stringify(payload), { mode: 0o600 });
  renameSync(tmp, target);
}

/** Single-writer guard so concurrent panes do not all hit the API at once. */
function acquireLock(env = process.env) {
  ensureCacheDir(env);
  const path = lockPath(env);
  try {
    mkdirSync(path);
    return true;
  } catch {
    try {
      if (Date.now() - statSync(path).mtimeMs > LOCK_STALE_MS) {
        rmSync(path, { recursive: true, force: true });
        mkdirSync(path);
        return true;
      }
    } catch {
      /* lost the race to another refresher */
    }
    return false;
  }
}

/**
 * Claim the right to refresh, for the caller to hand to a spawned child.
 *
 * The parent takes the lock *before* spawning: a child needs ~20ms to boot, and
 * during that window every further frame would otherwise see an unlocked cache
 * and spawn its own refresher. Taking it here makes the claim visible
 * immediately, so a burst of frames produces exactly one fetch.
 *
 * The caller MUST pass ownership to a process that releases it, or call
 * `abandonRefresh()` — otherwise the lock clears on its own after
 * `LOCK_STALE_MS`.
 */
export function claimRefresh(env = process.env) {
  return acquireLock(env);
}

export function abandonRefresh(env = process.env) {
  releaseLock(env);
}

function releaseLock(env = process.env) {
  try {
    rmSync(lockPath(env), { recursive: true, force: true });
  } catch {
    /* best effort */
  }
}

/**
 * Fetch and cache a fresh snapshot. Runs in the detached refresh process, so
 * every failure is swallowed — a stale bar beats a broken statusline.
 */
export async function refreshUsage(env = process.env, { lockHeld = false } = {}) {
  if (!lockHeld && !acquireLock(env)) return null;
  try {
    const token = readAccessToken(env);
    if (!token) {
      writeCache({ scoped: [], fetchedAt: Date.now(), error: "no-token" }, env);
      return null;
    }
    const parsed = parseUsage(await fetchUsage(token));
    writeCache(parsed, env);
    return parsed;
  } catch (error) {
    // Keep the previous buckets if we have them; only the timestamp moves, so
    // the next attempt waits a full TTL instead of hammering the endpoint.
    const previous = readCachedUsage(env);
    writeCache(
      { scoped: previous?.scoped ?? [], fetchedAt: Date.now(), error: error?.message ?? "fetch failed" },
      env,
    );
    return null;
  } finally {
    releaseLock(env);
  }
}
