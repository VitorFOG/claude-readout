/**
 * Tool-call counter.
 *
 * Session transcripts grow to megabytes, and the statusline renders on every
 * keystroke-ish event, so re-reading the whole file is not an option. We keep a
 * per-session cursor (byte offset + running count) and only ever read the bytes
 * appended since the last render.
 */

import { closeSync, existsSync, openSync, readSync, readFileSync, renameSync, statSync, writeFileSync } from "node:fs";
import { join } from "node:path";

import { cacheDir, ensureCacheDir } from "./config.mjs";

const TOOL_USE_MARKER = /"type"\s*:\s*"tool_use"/g;
/** Longest marker we could split across a read boundary; overlap by this much. */
const MARKER_SLACK = 32;

function cursorPath(sessionId, env = process.env) {
  const safe = String(sessionId).replace(/[^A-Za-z0-9._-]/g, "_");
  return join(cacheDir(env), `tools-${safe}.json`);
}

function readCursor(path) {
  try {
    const parsed = JSON.parse(readFileSync(path, "utf8"));
    if (typeof parsed?.offset === "number" && typeof parsed?.count === "number") return parsed;
  } catch {
    /* a missing or corrupt cursor just means we start from zero */
  }
  return null;
}

function writeCursor(path, cursor, env = process.env) {
  try {
    ensureCacheDir(env);
    const tmp = `${path}.${process.pid}.tmp`;
    writeFileSync(tmp, JSON.stringify(cursor));
    renameSync(tmp, path);
  } catch {
    /* without a cursor we simply recount next time */
  }
}

function countMarkers(text) {
  TOOL_USE_MARKER.lastIndex = 0;
  let count = 0;
  while (TOOL_USE_MARKER.exec(text) !== null) count += 1;
  return count;
}

/**
 * @returns {number|null} tool calls in this session, or null when the
 *   transcript is unavailable.
 */
export function countToolCalls(transcriptPath, sessionId, env = process.env) {
  if (!transcriptPath || !existsSync(transcriptPath)) return null;

  let size;
  try {
    size = statSync(transcriptPath).size;
  } catch {
    return null;
  }

  const path = cursorPath(sessionId ?? transcriptPath, env);
  const cached = readCursor(path);
  // A shrunken file means the transcript was rotated or replaced — recount.
  const resume = cached && cached.size <= size && cached.offset <= size ? cached : null;
  let offset = resume?.offset ?? 0;
  let count = resume?.count ?? 0;

  if (offset >= size) return count;

  let fd;
  try {
    fd = openSync(transcriptPath, "r");
    // Re-read a few bytes before the cursor so a marker straddling the previous
    // boundary is not missed; the overlap is trimmed at the first newline.
    // All arithmetic below stays in BYTES — transcripts contain multi-byte
    // UTF-8, so string indices would drift from file offsets.
    const start = Math.max(0, offset - MARKER_SLACK);
    const buffer = Buffer.allocUnsafe(size - start);
    const read = readSync(fd, buffer, 0, buffer.length, start);
    const view = buffer.subarray(0, read);

    let dataStart = offset - start;
    if (start < offset) {
      const firstNewline = view.indexOf(0x0a);
      if (firstNewline === -1) return count;
      dataStart = firstNewline + 1;
    }

    // Only count whole lines; a half-written final line is left for next time.
    const lastNewline = view.lastIndexOf(0x0a);
    if (lastNewline === -1 || lastNewline < dataStart) return count;
    count += countMarkers(view.toString("utf8", dataStart, lastNewline + 1));
    offset = start + lastNewline + 1;
  } catch {
    return count || null;
  } finally {
    if (fd !== undefined) {
      try {
        closeSync(fd);
      } catch {
        /* ignore */
      }
    }
  }

  writeCursor(path, { offset, count, size }, env);
  return count;
}
