#!/bin/sh
# Starts y for screenshots in a WezTerm window of its own, in the workspace
# "marmota-shots" that your window doesn't show, and prints its pane id.
#
#   ./start.sh [FOLDER]     FOLDER is $DEMO/tidepool by default
#
# Its settings, state, Claude Code transcripts and Codex home are the demo's
# (./demo.sh makes them), so nothing of yours shows. COLS and ROWS set the
# window's size: 160x44 by default.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
DEMO=${DEMO:-$HOME/code}
H=$DEMO/.shots
[ -d "$H" ] || { echo "no $H: run ./demo.sh first" >&2; exit 1; }
# Started from inside Claude Code, the CLAUDE* variables would reach the
# sessions y starts, and Claude Code turns transcripts off for those.
unset_claude=$(env | grep -E '^(AI_AGENT|CLAUDE[A-Z_]*)=' | sed 's/=.*//; s/^/-u /' | tr '\n' ' ')
# shellcheck disable=SC2086 # one word per -u
exec wezterm --config "initial_cols=${COLS:-160}" --config "initial_rows=${ROWS:-44}" cli spawn --new-window \
	--workspace marmota-shots --cwd "${1:-$DEMO/tidepool}" -- env $unset_claude \
	PATH="$here/wrappers:$PATH" DEMO="$DEMO" CLAUDE_CONFIG_DIR="$H/claude" CODEX_HOME="$H/codex" \
	XDG_CONFIG_HOME="$H/xdg/config" XDG_DATA_HOME="$H/xdg/data" XDG_STATE_HOME="$H/xdg/state" \
	XDG_CACHE_HOME="$H/xdg/cache" _ZO_DATA_DIR="$H/zoxide" TERM=xterm-256color "${Y:-$here/../../bin/y}"
