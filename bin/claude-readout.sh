#!/bin/sh
# Launcher for environments where `node` is not on PATH.
#
# Claude Code runs the statusline command through a non-interactive shell, which
# does not source the profile that puts nvm/fnm/volta's node on PATH. This finds
# a usable node and execs the HUD with it.
#
# Order: $READOUT_NODE, PATH, nvm, fnm, volta/asdf/nodenv, system paths.
# Exits 0 on failure so a missing node never blocks Claude Code.

case "$0" in
  */*) SCRIPT_DIR=${0%/*} ;;
  *) SCRIPT_DIR=. ;;
esac
SCRIPT_DIR=$(cd "$SCRIPT_DIR" 2>/dev/null && pwd -P) || SCRIPT_DIR=.
ENTRY="$SCRIPT_DIR/claude-readout.mjs"

if [ ! -f "$ENTRY" ]; then
  printf 'claude-readout: entry point missing at %s\n' "$ENTRY"
  exit 0
fi

NODE_BIN=""

if [ -n "${READOUT_NODE:-}" ] && [ -x "${READOUT_NODE}" ]; then
  NODE_BIN=$READOUT_NODE
fi

if [ -z "$NODE_BIN" ]; then
  NODE_BIN=$(command -v node 2>/dev/null)
fi

# nvm: no default alias guaranteed, so take the highest version directory.
if [ -z "$NODE_BIN" ] && [ -d "$HOME/.nvm/versions/node" ]; then
  for candidate in "$HOME/.nvm/versions/node/"*/bin/node; do
    [ -x "$candidate" ] && NODE_BIN=$candidate
  done
fi

if [ -z "$NODE_BIN" ]; then
  for candidate in \
    "$HOME/.fnm/aliases/default/bin/node" \
    "$HOME/.local/share/fnm/aliases/default/bin/node" \
    "$HOME/Library/Application Support/fnm/aliases/default/bin/node" \
    "$HOME/.volta/bin/node" \
    "$HOME/.asdf/shims/node" \
    "$HOME/.nodenv/shims/node" \
    /opt/homebrew/bin/node \
    /usr/local/bin/node \
    /usr/bin/node; do
    if [ -x "$candidate" ]; then
      NODE_BIN=$candidate
      break
    fi
  done
fi

if [ -z "$NODE_BIN" ]; then
  printf 'claude-readout: no node found (set READOUT_NODE to an absolute path)\n'
  exit 0
fi

exec "$NODE_BIN" "$ENTRY" "$@"
