# claude-hud

A Nerd Font statusline for [Claude Code](https://claude.com/claude-code). Model, rate-limit
meters, per-model weekly quotas, context usage, session time, tool calls and git — in truecolor,
on one line.

```
  feat/payments-api  !12 ?4
󰚩 Opus 5 1M │  5h ███░░░░░ 38% (3h55m) │  wk █░░░░░░░ 14% (3d1h) │ Fable ██░░░░░░ 22% (3d1h) │ 󰍛 ██░░░░░░░░ 23% │ 󰧑 xhigh │ 󰔟 29m │  108
```

- **Per-model weekly quotas.** Fable, and any tier Anthropic adds later, each with its own
  meter. These are not in the payload Claude Code hands the statusline, so showing them takes a
  call to the usage API — see [how it works](#how-the-per-model-quota-works).
- **No dependencies.** Pure Node. Nothing to install but the files.
- **Never blocks your prompt.** The render path does no network I/O at all; the usage snapshot
  refreshes in a detached process for the next frame.
- **Continuous meter colour.** Bars interpolate along a ramp instead of snapping between three
  fixed colours, so neighbouring values look like neighbours.

## Install

```sh
git clone https://github.com/VitorFOG/claude-hud ~/.local/src/claude-hud
```

Then point Claude Code at it — `~/.claude/settings.json`:

```json
{
  "statusLine": {
    "type": "command",
    "command": "node ~/.local/src/claude-hud/bin/claude-hud.mjs"
  }
}
```

If `node` isn't on `PATH` in non-interactive shells — which is the normal case for nvm, fnm and
volta users — point Claude Code at the launcher instead, which finds node itself:

```json
{ "statusLine": { "type": "command", "command": "sh ~/.local/src/claude-hud/bin/claude-hud.sh" } }
```

Set `CLAUDE_HUD_NODE` to an absolute path to override its search.

Requires Node 18+ and a [Nerd Font](https://www.nerdfonts.com/) in your terminal. Check the
glyphs render:

```sh
node bin/claude-hud.mjs --legend
```

Any box or blank means your font lacks that glyph — override it in the config, or switch the
whole set to plain text with `"glyphs": "text"`. The same table explains what every element
means, which is as close to a tooltip as a terminal statusline gets.

## What each element shows

| Glyph | Element | Source |
| --- | --- | --- |
| 󰚩 | Model, with a `1M` marker for the long-context variant | stdin |
|  | 5-hour usage window + time to reset | stdin |
|  | Weekly usage window + time to reset | stdin |
| *(name)* | Per-model weekly quota — printed as the model name, e.g. `Fable` | usage API |
| 󰍛 | Context window used | stdin |
| 󰧑 | Extended thinking + effort level | stdin |
| 󰔟 | Session duration | stdin |
|  | Tool calls this session | transcript |
|  | Branch, with working-tree counts | `git` |

Everything but the per-model quota comes straight from the JSON Claude Code hands the
statusline on stdin, which is why it's cheap.

## Configuration

Optional, at `~/.config/claude-hud/config.json` (or `$CLAUDE_HUD_CONFIG`). Every key is an
override — omit anything you don't care about.

```json
{
  "barWidth": 8,
  "contextBarWidth": 10,
  "showGitLine": true,
  "labels": "auto",
  "elements": {
    "cost": true,
    "repo": true,
    "tools": false
  },
  "palette": {
    "accent": "#7aa2f7",
    "scoped": "#bb9af7",
    "bar": [
      { "at": 0, "color": "#9ece6a" },
      { "at": 55, "color": "#9ece6a" },
      { "at": 80, "color": "#e0af68" },
      { "at": 100, "color": "#f7768e" }
    ]
  },
  "glyphs": { "model": "󰠮", "tools": "" }
}
```

**Meter colour.** `palette.bar` is a ramp, not a set of thresholds. Stops carry an `at`
percentage, and the colour interpolates between them — so a bar at 54% and one at 56% look
related instead of snapping from green to yellow. The default holds green through 55%, warms to
amber by 80%, and reaches red at 100%. A plain array of hex strings also works and is spread
evenly across 0–100.

**Elements.** Set any of `model`, `fiveHour`, `weekly`, `scoped`, `context`, `session`,
`thinking`, `tools`, `cost`, `repo`, `branch`, `gitStatus` to `false` to hide it (`cost` and
`repo` are off by default).

**Meter names.** `labels` controls whether a meter prints its name next to the glyph:

| Value | Effect |
| --- | --- |
| `"auto"` *(default)* | Names the quota meters — `5h`, `wk`, and each model-scoped bucket. These sit side by side and look alike, so an icon alone can't tell them apart. |
| `"always"` | Also names the context meter. |
| `"never"` | Glyphs only. |

Model-scoped buckets keep their model name under every setting: nothing else identifies which
model a bar belongs to, and for the same reason they carry no icon — one would just repeat the
name. Give them a glyph with `"glyphs": { "scoped": "" }` if you want one. Rename the built-ins
with `"names": { "fiveHour": "5h", "weekly": "week", "context": "ctx" }`.

**Other keys.** `usageApi: false` skips the usage request entirely — you keep the 5-hour and
weekly meters from stdin and lose only the per-model buckets. `usageTtlSeconds` (default 120)
controls how often that snapshot refreshes. `separator` overrides the `│` between elements.

Colour follows the [`NO_COLOR`](https://no-color.org/) convention and can be forced off with
`--no-color`.

## How the per-model quota works

Claude Code's stdin payload carries `rate_limits.five_hour` and `rate_limits.seven_day`, but not
the per-model weekly buckets. Those come from `GET api.anthropic.com/api/oauth/usage`, read with
the OAuth token Claude Code already stores (`~/.claude/.credentials.json`, or the login Keychain
on macOS). The response's `limits[]` array is walked for `kind: "weekly_scoped"` entries, each
labelled by `scope.model.display_name` — so a new model tier appears on its own, with no release
here.

That request never happens on the render path. `claude-hud` reads a cached snapshot from
`~/.cache/claude-hud/usage.json` and, if it's older than the TTL, spawns a detached
`--refresh-usage` process for the next frame. A cold cache costs you the per-model bars for one
frame and nothing else.

**It does not refresh the OAuth token.** That's Claude Code's job, and racing it risks
invalidating the credentials your editor is using. An expired token just means no per-model bars
until Claude Code renews it.

## Troubleshooting

```sh
node bin/claude-hud.mjs --doctor    # config path, token, cache age, colour support
node bin/claude-hud.mjs --legend    # what each element means, and a font check
node bin/claude-hud.mjs --refresh-usage   # force a usage fetch now
```

Render it by hand against a captured payload:

```sh
node bin/claude-hud.mjs < test/fixture-session.json
```

A render takes about 60ms, roughly 20ms of which is Node's own startup.

## Development

```sh
npm test    # node --test, no dependencies
```

CI runs the suite on Node 18/20/22, Linux and macOS.

The statusline contract is: read JSON on stdin, print to stdout, **always exit 0**. A statusline
that throws leaves an empty pane, so every I/O path here degrades to "show less" rather than
failing.

## Acknowledgements

The per-model weekly bucket handling was informed by the HUD in
[oh-my-claudecode](https://github.com/Yeachan-Heo/oh-my-claudecode) (MIT), which was the first
statusline to surface those buckets generically.

## License

MIT
