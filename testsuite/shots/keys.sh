#!/bin/sh
# Types into the y that start.sh started, one key per argument, 0.35 s apart,
# then prints its screen (QUIET=1: doesn't).
#
#   ./keys.sh PANE_ID z t i d e '\r'     \r enter, \x1b esc, '\t' tab, '\x1b[Z' shift+tab
set -eu
pane=$1
shift
for k in "$@"; do
	printf '%b' "$k" | wezterm cli send-text --pane-id "$pane" --no-paste
	sleep 0.35
done
sleep "${WAIT:-1}"
[ -n "${QUIET:-}" ] || wezterm cli get-text --pane-id "$pane"
