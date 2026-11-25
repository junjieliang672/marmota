#!/bin/bash

# Claude Code's status line, as Marmota sets it up: build.sh names this file
# as statusLine in Claude Code's settings.json, unless you have a status line
# of your own. One line, the colors your terminal's scheme has:
#
#   Fable 5.2  xhigh  fix-scroll   marmota   dev ●↑2  ctx 63%  wk 62% ↻ 3d4h
#
#   the model, its effort, the session's name when it has one
#   the folder, and in a git worktree the repository's folder and the worktree
#   the branch: green, or yellow with ● for changes not committed; ↑ ↓ are
#     the commits ahead of and behind its upstream
#   ctx: the context left; a snowflake after it when the prompt cache has
#     gone cold, so the next message reads the whole context again
#   wk: what is left of the week's limit, and how long until it resets
#
# What is left is green, yellow at 30% or less, red at 10% or less. A line
# wider than the pane loses the effort, then the time until the reset, the
# session's name and the folder.
#
# Claude Code runs this with the session as JSON on stdin and the pane's
# width in COLUMNS. It needs jq, and a Nerd Font for the icons.

input=$(cat)

# Everything the line shows, read by one jq (@sh quotes each value for eval)
model="" effort="" name="" cwd="" worktree="" ctx="" week="" resets="" cold=""
eval "$(jq -r '
    @sh "model=\(.model.display_name // "")",
    @sh "effort=\(.effort.level // "")",
    @sh "name=\(.session_name // "")",
    @sh "cwd=\(.workspace.current_dir // .cwd // "")",
    @sh "worktree=\(.workspace.git_worktree // .worktree.name // "")",
    @sh "ctx=\(.context_window.remaining_percentage | if . == null then "" else round end)",
    @sh "week=\(.rate_limits.seven_day.used_percentage | if . == null then "" else 100 - round end)",
    @sh "resets=\(.rate_limits.seven_day.resets_at | if . == null then "" else floor end)",
    @sh "cold=\(if .prompt_cache.caching_observed == true and .prompt_cache.warm == false then "1" else "" end)"
' <<< "$input" 2>/dev/null)"

# Git, in one run that takes no lock: the branch, what it is ahead and behind
# by, and whether a tracked file has changes
folder=${cwd##*/} branch="" oid="" dirty="" ahead=0 behind=0
if [ -n "$cwd" ] && [ -d "$cwd" ]; then
    while IFS= read -r entry; do
        case "$entry" in
            "# branch.oid "*) oid=${entry#\# branch.oid } ;;
            "# branch.head "*) branch=${entry#\# branch.head } ;;
            "# branch.ab "*)
                set -- ${entry#\# branch.ab }
                ahead=${1#+} behind=${2#-}
                ;;
            "#"*) ;;
            ?*) dirty=1 ;;
        esac
    done < <(git -C "$cwd" --no-optional-locks status --porcelain=v2 --branch --untracked-files=no 2>/dev/null)
    [ "$branch" = "(detached)" ] && branch=${oid:0:7}
    # In a worktree the folder is the repository's, not the worktree's
    if [ -n "$worktree" ]; then
        common=$(cd "$cwd" && cd "$(git rev-parse --git-common-dir 2>/dev/null)" 2>/dev/null && pwd -P)
        common=${common%/.git}
        [ -n "$common" ] && folder=${common##*/}
    fi
fi

# The colors are the terminal's own 16; text with none is the grey Claude Code
# gives a status line
esc=$'\033'
plain="$esc[0m" bright="$esc[97m" blue="$esc[34m" cyan="$esc[36m" green="$esc[32m" yellow="$esc[33m"
# The icons, as their bytes, so that the file reads in any font. A terminal
# draws one wider than its column, over what comes next: a space follows each
icon_folder=$'\xef\x81\xbb'       # nf-fa-folder
icon_branch=$'\xef\x90\x98'       # nf-oct-git_branch
icon_worktree=$'\xf3\xb0\x99\x85' # nf-md-file_tree
icon_cold=$'\xef\x8b\x9c'         # nf-fa-snowflake_o
icon_reset=$'\xf3\xb0\x91\x90'    # nf-md-refresh
dot=$'\xe2\x97\x8f' up=$'\xe2\x86\x91' down=$'\xe2\x86\x93'

# columns sets n to the columns a text takes: two for a character that isn't
# ASCII, as Chinese is drawn
columns() {
    local ascii=${1//[^[:ascii:]]/}
    n=$(( ${#1} * 2 - ${#ascii} ))
}

# left sets color to that of what is left: red at 10% or less, yellow at 30%
left() {
    if [ "$1" -le 10 ]; then
        color="$esc[1;31m"
    elif [ "$1" -le 30 ]; then
        color="$esc[1;33m"
    else
        color=$green
    fi
}

# until_reset sets eta to the time from now to $1, a time in seconds: 3d4h, 2h10m, 45m
until_reset() {
    local secs=$(( $1 - $(date +%s) ))
    [ "$secs" -lt 0 ] && secs=0
    if [ "$secs" -ge 86400 ]; then
        eta="$((secs / 86400))d$(( (secs % 86400) / 3600 ))h"
    elif [ "$secs" -ge 3600 ]; then
        eta="$((secs / 3600))h$(( (secs % 3600) / 60 ))m"
    else
        eta="$((secs / 60))m"
    fi
}

# part adds a part to the line, two spaces after the one before: $1 its
# columns, $2 its text
part() {
    [ -z "$out" ] || { out+="  "; width=$((width + 2)); }
    out+=$2$plain
    width=$((width + $1))
}

# build puts the line in out and its columns in width, without the parts
# that were given up (show_*)
build() {
    out="" width=0
    local n color eta text
    if [ -n "$model" ]; then
        columns "$model"
        part "$n" "$esc[1;35m$model"
    fi
    [ -z "$effort" ] || [ -z "$show_effort" ] || part "${#effort}" "$effort"
    if [ -n "$name" ] && [ -n "$show_name" ]; then
        columns "$name"
        part "$n" "$bright$name"
    fi
    if [ -n "$folder" ] && [ -n "$show_folder" ]; then
        columns "$folder"
        part $((n + 2)) "$blue$icon_folder $folder"
    fi
    if [ -n "$worktree" ]; then
        columns "$worktree"
        part $((n + 2)) "$cyan$icon_worktree $worktree"
    fi
    if [ -n "$branch" ]; then
        columns "$branch"
        n=$((n + 2))
        color=$green
        [ -z "$dirty" ] || color=$yellow
        text="$color$icon_branch $branch"
        local marks=""
        [ -z "$dirty" ] || { marks+="$yellow$dot"; n=$((n + 1)); }
        [ "$ahead" -gt 0 ] 2>/dev/null && { marks+="$cyan$up$ahead"; n=$((n + 1 + ${#ahead})); }
        [ "$behind" -gt 0 ] 2>/dev/null && { marks+="$cyan$down$behind"; n=$((n + 1 + ${#behind})); }
        [ -z "$marks" ] || { text+=" $marks"; n=$((n + 1)); }
        part "$n" "$text"
    fi
    if [ -n "$ctx" ]; then
        left "$ctx"
        text="ctx $color$ctx%"
        n=$((5 + ${#ctx}))
        [ -z "$cold" ] || { text+=" $plain$cyan$icon_cold"; n=$((n + 2)); }
        part "$n" "$text"
    fi
    if [ -n "$week" ]; then
        left "$week"
        text="wk $color$week%"
        n=$((4 + ${#week}))
        if [ -n "$resets" ] && [ -n "$show_reset" ]; then
            until_reset "$resets"
            text+="$plain $icon_reset $eta"
            n=$((n + 3 + ${#eta}))
        fi
        part "$n" "$text"
    fi
}

# Claude Code cuts a line wider than the pane less four columns: give parts
# up, in this order, until it fits
show_effort=1 show_reset=1 show_name=1 show_folder=1
build
for give_up in effort reset name folder; do
    [ -n "$COLUMNS" ] && [ "$width" -gt $((COLUMNS - 4)) ] || break
    eval "show_$give_up="
    build
done
printf '%s\n' "$out"
