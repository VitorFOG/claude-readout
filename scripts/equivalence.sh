#!/usr/bin/env bash
# Diff the Go binary against the Node implementation it replaced, over a matrix
# of payloads, configs, terminal widths, git states and flags. The Node version
# is checked out from git at the last commit that shipped it, so this stays
# runnable after the JavaScript is gone from the tree.
#
#   scripts/equivalence.sh [path/to/claude-readout]
#
# Exit 0 when every case matches byte for byte. Outputs are left under
# $EQUIV_DIR (default: a fresh mktemp dir) as out/ref and out/cand for diffing.
#
# Every config in the matrix is well-typed. A value of the wrong JSON type is
# where the two part ways by design: Go keeps the default for that key, Node
# coerced whatever it found. That class is out of scope here.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
REF_SHA=4a7f521
WORK=${EQUIV_DIR:-$(mktemp -d)}
mkdir -p "$WORK"
CAND=${1:-$WORK/claude-readout}
if [ ! -x "$CAND" ]; then
  (cd "$ROOT" && go build -o "$CAND" .)
fi

mkdir -p "$WORK/ref"
git -C "$ROOT" archive "$REF_SHA" bin src | tar -x -C "$WORK/ref"
REF=(node "$WORK/ref/bin/claude-readout.mjs")

now=$(date +%s)
now_ms=$((now * 1000))

# --- fixtures -----------------------------------------------------------------

mkdir -p "$WORK/payloads" "$WORK/configs" "$WORK/claude"

git_repo() {
  local dir=$1
  mkdir -p "$dir"
  git -C "$dir" init -q -b main
  git -C "$dir" config user.email eq@example.com
  git -C "$dir" config user.name eq
}

git_repo "$WORK/repo"
printf 'a\n' > "$WORK/repo/committed.txt"
printf 'b\n' > "$WORK/repo/staged.txt"
git -C "$WORK/repo" add committed.txt
git -C "$WORK/repo" commit -q -m one
git -C "$WORK/repo" clone -q --bare "$WORK/repo" "$WORK/remote.git"
git -C "$WORK/repo" remote add origin "$WORK/remote.git"
git -C "$WORK/repo" fetch -q origin
git -C "$WORK/repo" branch -q -u origin/main
printf 'c\n' >> "$WORK/repo/committed.txt"
git -C "$WORK/repo" commit -q -am two
printf 'd\n' >> "$WORK/repo/committed.txt"
git -C "$WORK/repo" add staged.txt
printf 'u1\n' > "$WORK/repo/untracked1.txt"
printf 'u2\n' > "$WORK/repo/untracked2.txt"

git_repo "$WORK/detached"
printf 'x\n' > "$WORK/detached/f"
git -C "$WORK/detached" add f
git -C "$WORK/detached" commit -q -m one
git -C "$WORK/detached" checkout -q --detach

git_repo "$WORK/fresh"

mkdir -p "$WORK/plain"

: > "$WORK/transcript.jsonl"
for i in $(seq 1 17); do
  printf '{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash"}]},"text":"日本語 — %d"}\n' "$i" >> "$WORK/transcript.jsonl"
  printf '{"type":"user","message":{"content":[{"type":"text","text":"hi %d"}]}}\n' "$i" >> "$WORK/transcript.jsonl"
done

fixture() {
  sed -e "s|1788386400|$((now + 14100))|" \
      -e "s|1788638400|$((now + 262800))|" \
      -e "s|/home/you/.claude/projects/-home-you-src/00000000-0000-4000-8000-000000000000.jsonl|$WORK/transcript.jsonl|" \
      -e "s|/home/you/src/example|$1|g" \
      "$ROOT/testdata/fixture-session.json"
}

fixture "$WORK/repo" > "$WORK/payloads/session.json"
fixture "$WORK/detached" > "$WORK/payloads/detached.json"
fixture "$WORK/fresh" > "$WORK/payloads/fresh.json"
fixture "$WORK/plain" > "$WORK/payloads/plain.json"
printf '{}' > "$WORK/payloads/empty-object.json"
: > "$WORK/payloads/empty.json"
printf 'not json at all' > "$WORK/payloads/garbage.json"
printf 'null' > "$WORK/payloads/null.json"
printf '{"model":{"display_name":"Sonnet 5"}}' > "$WORK/payloads/minimal.json"
printf '{"model":{"id":"claude-haiku-4-5"},"cwd":"%s","cost":{"total_cost_usd":0.125,"total_duration_ms":45000},"thinking":{"enabled":false},"context_window":{"used_percentage":99.6,"context_window_size":200000},"rate_limits":{"five_hour":{"used_percentage":0,"resets_at":"%s"},"seven_day":{"used_percentage":100}}}' \
  "$WORK/repo" "$(date -u -d "@$((now + 7200))" +%Y-%m-%dT%H:%M:%SZ)" > "$WORK/payloads/edges.json"
printf '{"model":{"display_name":"Opus 5"},"rate_limits":{"five_hour":{"used_percentage":50,"resets_at":%s}}}' "$((now - 60))" > "$WORK/payloads/past-reset.json"

printf '{"glyphs":"text","separator":"|"}' > "$WORK/configs/text.json"
printf '{"labels":"always"}' > "$WORK/configs/always.json"
printf '{"labels":"never","glyphs":{"fiveHour":"@","weekly":"%%","scoped":"*"}}' > "$WORK/configs/never.json"
printf '{"overflow":"wrap"}' > "$WORK/configs/wrap.json"
printf '{"overflow":"none"}' > "$WORK/configs/none.json"
printf '{"elements":{"cost":true,"repo":true,"tools":false,"thinking":false}}' > "$WORK/configs/elements.json"
printf '{"palette":{"bar":["#000000","#ffffff"],"accent":"#ff0000"},"barWidth":3,"contextBarWidth":4}' > "$WORK/configs/palette.json"
printf '{"palette":{"bar":[{"at":0,"hex":"#112233"},{"color":"#445566"},{"at":50,"color":"nope"}],"muted":"#abc"}}' > "$WORK/configs/palette-objects.json"
printf '{not json' > "$WORK/configs/invalid.json"
printf '{"usageApi":false,"showGitLine":false}' > "$WORK/configs/nousage.json"
printf '{"names":{"fiveHour":"five","context":"context"},"labels":"always","reserveColumns":5,"shedOrder":["model","tools"]}' > "$WORK/configs/names.json"
printf '{"glyphs":{"model":"M","separator":"::","context":""}}' > "$WORK/configs/glyph-overrides.json"
printf '{"elements":{"fiveHour":false,"weekly":false,"scoped":false,"context":false,"session":false,"thinking":false,"tools":false,"model":false,"branch":false,"gitStatus":false}}' > "$WORK/configs/nothing.json"

# --- environments -------------------------------------------------------------

seed_cache() {
  local home=$1 fetched=$2
  mkdir -p "$home/cache/claude-readout"
  printf '{"scoped":[{"id":"fable","label":"Fable","percent":22,"resetsAt":"%s","severity":"normal","isActive":true},{"id":"opus","label":"Opus","percent":7,"resetsAt":null,"severity":null,"isActive":false}],"fetchedAt":%s}' \
    "$(date -u -d "@$((now + 262800))" +%Y-%m-%dT%H:%M:%SZ)" "$fetched" > "$home/cache/claude-readout/usage.json"
}

for impl in ref cand; do
  seed_cache "$WORK/home-$impl" "$now_ms"
  mkdir -p "$WORK/out/$impl"
done

run_case() {
  local name=$1 impl=$2 cols=$3 cfg=$4 payload=$5 nocolor=$6
  shift 6
  local -a cmd
  if [ "$impl" = ref ]; then cmd=("${REF[@]}"); else cmd=("$CAND"); fi
  local input=/dev/null
  [ -n "$payload" ] && input="$WORK/payloads/$payload"
  local cfgpath="$WORK/configs/$cfg"
  [ "$cfg" = default ] && cfgpath="$WORK/configs/does-not-exist.json"
  (
    cd "$WORK/repo"
    env -i PATH="$PATH" HOME="$WORK/home-$impl" \
      XDG_CONFIG_HOME="$WORK/shared-config" XDG_CACHE_HOME="$WORK/home-$impl/cache" \
      CLAUDE_CONFIG_DIR="$WORK/claude" READOUT_CONFIG="$cfgpath" \
      ${cols:+COLUMNS="$cols"} ${nocolor:+NO_COLOR=1} TERM=xterm-256color \
      "${cmd[@]}" "$@" < "$input" > "$WORK/out/$impl/$name" 2>&1 || echo "exit $?" >> "$WORK/out/$impl/$name"
  )
}

# --- matrix -------------------------------------------------------------------

for impl in ref cand; do
  for cfg in default text.json always.json never.json wrap.json none.json elements.json palette.json palette-objects.json invalid.json nousage.json names.json glyph-overrides.json nothing.json; do
    for cols in "" 200 130 110 94 80 65 50 40 30 20 abc; do
      run_case "render-${cfg%.json}-cols${cols:-unset}" "$impl" "$cols" "$cfg" session.json ""
    done
    run_case "ramp-${cfg%.json}" "$impl" "" "$cfg" "" "" --ramp
    run_case "doctor-${cfg%.json}" "$impl" "" "$cfg" "" "" --doctor
    # The Go --doctor adds "binary:" and "nerd font:" lines the Node version
    # never had, the cache age counts seconds between the two runs, and the two
    # JSON parsers word a syntax error differently.
    sed -i -e '/^binary: /d' -e '/^nerd font: /d' -e 's/[0-9]*s old$/Ns old/' -e 's/(INVALID: .*)$/(INVALID)/' "$WORK/out/$impl/doctor-${cfg%.json}"
  done
  for payload in detached.json fresh.json plain.json empty-object.json empty.json garbage.json null.json minimal.json edges.json past-reset.json; do
    for cols in "" 60; do
      run_case "payload-${payload%.json}-cols${cols:-unset}" "$impl" "$cols" default "$payload" ""
    done
  done
  for cols in "" 80; do
    run_case "nocolor-cols${cols:-unset}" "$impl" "$cols" default session.json 1
    run_case "nocolor-flag-cols${cols:-unset}" "$impl" "$cols" default session.json "" --no-color
  done
  run_case legend "$impl" "" default "" "" --legend
  run_case glyphs "$impl" "" default "" "" --glyphs
  run_case ramp-nocolor "$impl" "" default "" "" --ramp --no-color
  cp "$WORK/home-$impl/cache/claude-readout/tools-00000000-0000-4000-8000-000000000000.json" "$WORK/out/$impl/tools-cursor.json"
done

# Grow the transcript and render once more, so the cursor's resume path is
# compared and not only the first full count.
for i in 18 19; do
  printf '{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read"}]},"text":"more %d"}\n' "$i" >> "$WORK/transcript.jsonl"
done
for impl in ref cand; do
  run_case transcript-append "$impl" 100 default session.json ""
  cp "$WORK/home-$impl/cache/claude-readout/tools-00000000-0000-4000-8000-000000000000.json" "$WORK/out/$impl/tools-cursor-after-append.json"
done

# A stale cache makes both spawn a detached refresh; with no token that writes
# an empty snapshot with error "no-token" and releases the lock.
for impl in ref cand; do
  seed_cache "$WORK/home-$impl" 0
  run_case stale-render "$impl" 100 default session.json ""
done
sleep 2
for impl in ref cand; do
  sed 's/"fetchedAt":[0-9]*/"fetchedAt":X/' "$WORK/home-$impl/cache/claude-readout/usage.json" > "$WORK/out/$impl/usage-after-refresh.json"
  [ -e "$WORK/home-$impl/cache/claude-readout/usage.lock" ] && echo "lock still held" > "$WORK/out/$impl/lock-leak"
done

# The Node version clipped the git line by UTF-16 code units, so a cut that
# landed inside a Nerd Font glyph (outside the BMP) left half a surrogate pair,
# which prints as U+FFFD. Go cuts whole runes there. Drop those lines from both
# sides and say how many went; every other cell in the matrix still has to match.
dropped=0
for ref in "$WORK"/out/ref/*; do
  cand="$WORK/out/cand/$(basename "$ref")"
  broken=$( (grep -n $'\xef\xbf\xbd' "$ref" || true) | cut -d: -f1 | sed 's/$/d/' | tr '\n' ';')
  if [ -n "$broken" ]; then
    dropped=$((dropped + $(printf '%s' "$broken" | tr -cd ';' | wc -c)))
    sed -i "$broken" "$ref"
    sed -i "$broken" "$cand"
  fi
done

# --- verdict ------------------------------------------------------------------

if diff -r "$WORK/out/ref" "$WORK/out/cand" > "$WORK/diff.txt"; then
  echo "equivalence: $(ls "$WORK/out/ref" | wc -l) cases identical, $dropped git-line clips dropped for the Node half-glyph bug ($WORK)"
else
  echo "equivalence: MISMATCH, see $WORK/diff.txt"
  grep -c '^diff ' "$WORK/diff.txt" | sed 's/^/files differing: /'
  exit 1
fi
