import assert from "node:assert/strict";
import { mkdtempSync, writeFileSync, appendFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { after, describe, it } from "node:test";

import { parseHex, rampColor, setColorEnabled, stripAnsi } from "../src/color.mjs";
import { formatDuration, formatUntil, renderHud, visibleWidth } from "../src/render.mjs";
import { parseScopedBuckets } from "../src/usage.mjs";
import { countToolCalls } from "../src/transcript.mjs";
import { DEFAULT_CONFIG } from "../src/theme.mjs";

const scratch = mkdtempSync(join(tmpdir(), "claude-hud-test-"));
after(() => rmSync(scratch, { recursive: true, force: true }));

describe("color", () => {
  it("parses shorthand and full hex", () => {
    assert.deepEqual(parseHex("#fff"), [255, 255, 255]);
    assert.deepEqual(parseHex("7aa2f7"), [122, 162, 247]);
  });

  it("falls back to a readable grey on malformed hex", () => {
    assert.deepEqual(parseHex("nope"), [221, 221, 221]);
  });

  it("holds the first colour across a flat ramp segment", () => {
    const stops = [
      { at: 0, color: "#9ece6a" },
      { at: 55, color: "#9ece6a" },
      { at: 100, color: "#f7768e" },
    ];
    assert.deepEqual(rampColor(0, stops), [158, 206, 106]);
    assert.deepEqual(rampColor(40, stops), [158, 206, 106]);
    assert.deepEqual(rampColor(100, stops), [247, 118, 142]);
  });

  it("interpolates between stops", () => {
    const mid = rampColor(50, ["#000000", "#ffffff"]);
    assert.deepEqual(mid.map(Math.round), [128, 128, 128]);
  });

  it("clamps out-of-range percentages", () => {
    const stops = ["#000000", "#ffffff"];
    assert.deepEqual(rampColor(-20, stops), [0, 0, 0]);
    assert.deepEqual(rampColor(500, stops), [255, 255, 255]);
    assert.deepEqual(rampColor(NaN, stops), [0, 0, 0]);
  });
});

describe("formatting", () => {
  it("formats durations by magnitude", () => {
    assert.equal(formatDuration(30_000), "30s");
    assert.equal(formatDuration(20 * 60_000), "20m");
    assert.equal(formatDuration(65 * 60_000), "1h5m");
    assert.equal(formatDuration(50 * 3600_000), "2d2h");
  });

  it("rejects nonsense durations", () => {
    assert.equal(formatDuration(-1), null);
    assert.equal(formatDuration(undefined), null);
  });

  it("accepts both epoch seconds and ISO for reset times", () => {
    const inTwoHours = Date.now() + 2 * 3600_000;
    // Epoch seconds truncate sub-second precision, so the two shapes may land
    // on either side of the minute boundary — both are correct.
    const fromEpoch = formatUntil(Math.floor(inTwoHours / 1000));
    const fromIso = formatUntil(new Date(inTwoHours).toISOString());
    assert.match(fromEpoch, /^(1h59m|2h)$/);
    assert.match(fromIso, /^(1h59m|2h)$/);
  });

  it("returns null for a reset that already happened", () => {
    assert.equal(formatUntil(Date.now() / 1000 - 60), null);
    assert.equal(formatUntil(null), null);
    assert.equal(formatUntil("not a date"), null);
  });

  it("measures visible width without ANSI", () => {
    assert.equal(stripAnsi("\x1b[32mok\x1b[0m"), "ok");
    assert.equal(visibleWidth("\x1b[32mok\x1b[0m"), 2);
    assert.equal(visibleWidth("🔧"), 2);
  });
});

describe("usage: scoped weekly buckets", () => {
  const response = {
    limits: [
      { kind: "session", percent: 36, resets_at: "2026-09-02T22:00:00Z", scope: null },
      { kind: "weekly_all", percent: 14, resets_at: "2026-09-05T20:00:00Z", scope: null },
      {
        kind: "weekly_scoped",
        percent: 22,
        severity: "normal",
        resets_at: "2026-09-05T20:00:00Z",
        scope: { model: { id: null, display_name: "Fable" } },
        is_active: false,
      },
    ],
  };

  it("extracts only weekly_scoped entries", () => {
    const buckets = parseScopedBuckets(response);
    assert.equal(buckets.length, 1);
    assert.equal(buckets[0].label, "Fable");
    assert.equal(buckets[0].percent, 22);
    assert.equal(buckets[0].severity, "normal");
  });

  it("prefers the active duplicate", () => {
    const buckets = parseScopedBuckets({
      limits: [
        {
          kind: "weekly_scoped",
          percent: 10,
          scope: { model: { display_name: "Fable" } },
          is_active: false,
        },
        {
          kind: "weekly_scoped",
          percent: 55,
          scope: { model: { display_name: "fable" } },
          is_active: true,
        },
      ],
    });
    assert.equal(buckets.length, 1);
    assert.equal(buckets[0].percent, 55);
  });

  it("survives absent, malformed and unnamed entries", () => {
    assert.deepEqual(parseScopedBuckets({}), []);
    assert.deepEqual(parseScopedBuckets({ limits: "nope" }), []);
    assert.deepEqual(
      parseScopedBuckets({
        limits: [
          null,
          { kind: "weekly_scoped", percent: "x", scope: { model: { display_name: "A" } } },
          { kind: "weekly_scoped", percent: 5, scope: { model: { display_name: "  " } } },
        ],
      }),
      [],
    );
  });

  it("clamps percentages into 0-100", () => {
    const [bucket] = parseScopedBuckets({
      limits: [
        { kind: "weekly_scoped", percent: 140, scope: { model: { display_name: "Over" } } },
      ],
    });
    assert.equal(bucket.percent, 100);
  });
});

describe("transcript tool counting", () => {
  const env = { XDG_CACHE_HOME: join(scratch, "cache") };

  it("counts across incremental appends without double counting", () => {
    const path = join(scratch, "session.jsonl");
    const toolLine = JSON.stringify({ content: [{ type: "tool_use", name: "Bash" }] });
    const plainLine = JSON.stringify({ content: [{ type: "text", text: "hi" }] });
    writeFileSync(path, `${toolLine}\n${plainLine}\n`);
    assert.equal(countToolCalls(path, "s1", env), 1);

    appendFileSync(path, `${toolLine}\n${toolLine}\n`);
    assert.equal(countToolCalls(path, "s1", env), 3);

    // No growth: the count must stay put rather than re-scan.
    assert.equal(countToolCalls(path, "s1", env), 3);
  });

  it("ignores a partially written trailing line until it is complete", () => {
    const path = join(scratch, "partial.jsonl");
    const toolLine = JSON.stringify({ content: [{ type: "tool_use" }] });
    writeFileSync(path, `${toolLine}\n`);
    assert.equal(countToolCalls(path, "s2", env), 1);

    appendFileSync(path, '{"content":[{"type":"tool_u');
    assert.equal(countToolCalls(path, "s2", env), 1);

    appendFileSync(path, 'se"}]}\n');
    assert.equal(countToolCalls(path, "s2", env), 2);
  });

  it("handles multi-byte content without drifting the byte cursor", () => {
    const path = join(scratch, "utf8.jsonl");
    const wide = JSON.stringify({ text: "日本語テキスト — em dash", content: [{ type: "tool_use" }] });
    writeFileSync(path, `${wide}\n`);
    assert.equal(countToolCalls(path, "s3", env), 1);
    appendFileSync(path, `${wide}\n`);
    assert.equal(countToolCalls(path, "s3", env), 2);
  });

  it("returns null when there is no transcript", () => {
    assert.equal(countToolCalls(join(scratch, "missing.jsonl"), "s4", env), null);
    assert.equal(countToolCalls(undefined, "s5", env), null);
  });
});

describe("render", () => {
  setColorEnabled(false); // assert on text, not escape codes

  const payload = {
    model: { id: "claude-opus-5[1m]", display_name: "Opus 5 (1M context)" },
    context_window: { used_percentage: 23, context_window_size: 1_000_000 },
    cost: { total_duration_ms: 29 * 60_000, total_cost_usd: 8.7 },
    thinking: { enabled: true },
    effort: { level: "xhigh" },
    rate_limits: {
      five_hour: { used_percentage: 38, resets_at: Math.floor(Date.now() / 1000) + 3600 },
      seven_day: { used_percentage: 14, resets_at: Math.floor(Date.now() / 1000) + 86_400 },
    },
  };

  const config = { ...DEFAULT_CONFIG, glyphs: "text", separator: "|" };

  it("renders a glyphless meter without a leading space", () => {
    const out = renderHud({
      payload: { rate_limits: {} },
      config: { ...config, glyphs: { scoped: "" } },
      git: null,
      scoped: [{ id: "fable", label: "Fable", percent: 22, resetsAt: null }],
      toolCalls: null,
    });
    assert.equal(out, "Fable ██░░░░░░ 22%");
  });

  it("names the quota meters so look-alike bars stay identifiable", () => {
    const out = renderHud({
      payload,
      config: { ...config, glyphs: { fiveHour: "@", weekly: "@", scoped: "@", context: "@" } },
      git: null,
      scoped: [{ id: "fable", label: "Fable", percent: 22, resetsAt: null }],
      toolCalls: null,
    });
    // Same glyph on all four: only the names tell them apart.
    assert.match(out, /@ 5h .*38%/);
    assert.match(out, /@ wk .*14%/);
    assert.match(out, /@ Fable .*22%/);
    assert.match(out, /@ .*23%/, "context stays bare in auto mode");
    assert.equal(out.includes("ctx"), false);
  });

  it('names every meter under labels: "always"', () => {
    const out = renderHud({
      payload,
      config: { ...config, labels: "always" },
      git: null,
      scoped: [],
      toolCalls: null,
    });
    assert.match(out, /ctx .*23%/);
  });

  it('keeps the model name on scoped buckets even under labels: "never"', () => {
    // Distinct glyphs that are not themselves names, so the assertion tests the
    // label and not the "text" glyph set (whose glyphs are the names).
    const out = renderHud({
      payload,
      config: { ...config, labels: "never", glyphs: { fiveHour: "@", weekly: "%", scoped: "*" } },
      git: null,
      scoped: [{ id: "fable", label: "Fable", percent: 22, resetsAt: null }],
      toolCalls: null,
    });
    assert.match(out, /\* Fable .*22%/, "a scoped bar is unidentifiable without its model name");
    assert.equal(out.includes("5h"), false);
    assert.equal(out.includes("wk"), false);
  });

  it("renders the meter line with every element", () => {
    const out = renderHud({
      payload,
      config,
      git: null,
      scoped: [{ id: "fable", label: "Fable", percent: 22, resetsAt: null }],
      toolCalls: 108,
　　});
    assert.match(out, /model Opus 5/);
    assert.match(out, /5h .*38%/);
    assert.match(out, /wk .*14%/);
    assert.match(out, /Fable .*22%/);
    assert.match(out, /ctx .*23%/);
    assert.match(out, /think xhigh/);
    assert.match(out, /up 29m/);
    assert.match(out, /tools 108/);
    assert.equal(out.includes("\n"), false, "no git line without a repo");
  });

  it("adds a git line when in a repository", () => {
    const out = renderHud({
      payload,
      config,
      git: { repo: "BionD", branch: "main", staged: 0, modified: 12, untracked: 4, ahead: 2, behind: 0 },
      scoped: [],
      toolCalls: null,
    });
    const [gitLine] = out.split("\n");
    assert.match(gitLine, /branch main/);
    assert.match(gitLine, /!12/);
    assert.match(gitLine, /\?4/);
    assert.match(gitLine, /\^2/);
  });

  it("omits elements with no data instead of leaving empty separators", () => {
    const out = renderHud({
      payload: { model: { display_name: "Sonnet 5" } },
      config,
      git: null,
      scoped: [],
      toolCalls: null,
    });
    assert.equal(out, "model Sonnet 5");
  });

  it("drops trailing elements to fit the terminal width", () => {
    const wide = renderHud({ payload, config, git: null, scoped: [], toolCalls: 108 });
    const narrow = renderHud({ payload, config, git: null, scoped: [], toolCalls: 108, columns: 40 });
    assert.ok(visibleWidth(narrow) <= visibleWidth(wide));
    assert.ok(narrow.startsWith("model Opus 5"), "keeps the highest-priority element");
  });

  it("survives an empty payload", () => {
    const out = renderHud({ payload: {}, config, git: null, scoped: [], toolCalls: null });
    assert.equal(typeof out, "string");
  });
});
