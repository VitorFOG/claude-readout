#!/usr/bin/env bash
# Install the freshly built packages the way a user would, from local tarballs
# into a throwaway prefix, then prove both entry points: the `claude-readout`
# command npm puts on PATH, and the native binary `--setup` writes into Claude
# Code's settings.
#
#   node scripts/build-npm.mjs && scripts/install-smoke.sh
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

platform=$(node -p 'process.platform')
arch=$(node -p 'process.arch')
pkg="claude-readout-$platform-$arch"

# Installing both tarballs in one command lets the platform package satisfy the
# root's optional dependency without a registry; the other five are optional
# and simply fail to resolve.
platform_tarball=$(cd "$WORK" && npm pack -s "$ROOT/dist/npm/$pkg")
root_tarball=$(cd "$WORK" && npm pack -s "$ROOT")
# The prefix has a space in it so the path --setup writes needs quoting.
prefix="$WORK/pre fix"
mkdir -p "$prefix"
npm install -s -g --prefix "$prefix" "$WORK/$platform_tarball" "$WORK/$root_tarball"

command_on_path="$prefix/bin/claude-readout"
[ -x "$command_on_path" ] || { echo "no executable at $command_on_path"; exit 1; }

out=$(NO_COLOR=1 HOME="$WORK/home" "$command_on_path" < "$ROOT/testdata/fixture-session.json")
echo "$out"
echo "$out" | grep -q 'Opus 5' || { echo "render through the npm command did not mention the model"; exit 1; }

HOME="$WORK/home" "$command_on_path" --setup
command=$(node -p 'JSON.parse(require("fs").readFileSync(process.argv[1],"utf8")).statusLine.command' "$WORK/home/.claude/settings.json")
# Claude Code hands the command to a shell, so run it the same way.
NO_COLOR=1 HOME="$WORK/home" sh -c "$command" < "$ROOT/testdata/fixture-session.json" | grep -q 'Opus 5' || { echo "the command --setup wrote does not render: $command"; exit 1; }
binary=$(HOME="$WORK/home" sh -c "$command --doctor" | sed -n 's/^binary: *//p')
# The binary reports its physical path; on macOS /var is a symlink to /private/var.
prefix_real=$(cd "$prefix" && pwd -P)
case "$binary" in
  "$prefix"/*|"$prefix_real"/*) ;;
  *) echo "--setup pointed outside the install prefix: $binary"; exit 1 ;;
esac
if head -c 2 "$binary" | grep -q '#!'; then
  echo "--setup pointed at a script, not the binary: $binary"
  exit 1
fi

echo "install smoke: ok ($command)"
