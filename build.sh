#!/usr/bin/env bash

# Builds y (Marmota) into ./bin/y, and sets up your shell to run it: a block
# in your shell config puts ./bin on PATH and defines the y function, which
# moves your shell to the folder y quit in when cd_on_quit is on. On a Mac it
# also keeps Marmota's look in ~/.wezterm.lua up to date. Where Claude Code
# is, it sets up its look too: its theme and status line (scripts/claude). An
# install from before the program was Marmota and its binary y is moved over:
# its shell lines and its binaries go (OLD_* below). On a Mac where Marmota was
# never installed (no Go, or nothing in your shell config), it first installs
# everything through install-deps-macos.sh, which calls back here to build.
#
#   ./build.sh                  build; the first time, install everything
#   ./build.sh --install [...]  install everything again; the rest of the
#                               arguments go to install-deps-macos.sh
#                               (--minimal, --dry-run, --skip-ffmpeg, ...)
#   ./build.sh --no-install     only build (and set up the shell)
#
# Y_BUILD_ONLY=1 does what --no-install does; install-deps-macos.sh sets it
# when it calls back here.

cd "$(dirname "${BASH_SOURCE[0]}")" || exit 1
REPO="$(pwd)"
TARGET="$REPO/bin/y"

INSTALL=auto
INSTALL_ARGS=()
case "$1" in
    --install)
        INSTALL=yes
        shift
        INSTALL_ARGS=("$@")
        ;;
    --no-install)
        INSTALL=no
        ;;
    -h|--help)
        sed -n '3,20p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
        exit 0
        ;;
    "")
        ;;
    *)
        echo "Unknown option: $1 (see ./build.sh --help)"
        exit 1
        ;;
esac
if [[ -n "$Y_BUILD_ONLY" ]]; then
    INSTALL=no
fi

SHELL_CONFIGS=("$HOME/.zshrc" "$HOME/.bashrc" "$HOME/.config/fish/config.fish")
BLOCK_BEGIN="# >>> marmota >>>"
BLOCK_END="# <<< marmota <<<"
# What an install from before the program was Marmota left on a computer.
# These lines are the only ones that say its old names, superfile and spf:
# they are what is looked for, to take it out.
#   the same block, as it was marked then
LEGACY_BEGIN="# >>> y (superfile) >>>"
LEGACY_END="# <<< y (superfile) <<<"
#   the block from before y, which ended in an alias of y to the old binary
OLD_MARKER="Superfile cd-on-quit wrapper"
#   an alias of y, or of the old binary's name, to the old binary
OLD_NAME="spf"
OLD_ALIAS="^alias (y|$OLD_NAME)[ =].*$OLD_NAME"
#   the variable that block named the old binary with, and what it said of itself
OLD_BINARY_VAR="SPF_BINARY"
OLD_VERSION_LINE="^superfile version"

# installed_before reports a shell config that runs Marmota, old or new.
installed_before() {
    grep -qsF -e "$BLOCK_BEGIN" -e "$LEGACY_BEGIN" -e "$OLD_MARKER" "${SHELL_CONFIGS[@]}" ||
        grep -qsE "$OLD_ALIAS" "${SHELL_CONFIGS[@]}"
}

# Never installed: no Go, or no shell config that runs Marmota
if [[ $INSTALL == auto ]]; then
    INSTALL=no
    if [[ $(uname -s) == "Darwin" ]] && { ! command -v go &>/dev/null || ! installed_before; }; then
        INSTALL=yes
        echo "Marmota isn't installed yet: installing everything first (./build.sh --no-install only builds)"
    fi
fi

if [[ $INSTALL == yes ]]; then
    if [[ $(uname -s) != "Darwin" ]]; then
        echo "Installing is macOS only; ./build.sh --no-install builds, and README.md lists what to install"
        exit 1
    fi
    exec ./install-deps-macos.sh "${INSTALL_ARGS[@]}"
fi

# y_block prints the block for a shell config: sh for zsh and bash, or fish.
y_block() {
    echo "$BLOCK_BEGIN"
    echo "# y is Marmota (build.sh writes this). The y function runs it and, with"
    echo "# cd_on_quit on, moves your shell to the folder it quit in."
    if [[ $1 == fish ]]; then
        cat << EOF
set -gx PATH "$REPO/bin" \$PATH
function y
    command y \$argv
    set -l last_dir (command y path-list --lastdir-file 2>/dev/null)
    if test -n "\$last_dir"; and test -f "\$last_dir"
        source "\$last_dir"
        rm -f -- "\$last_dir"
    end
end
EOF
    else
        cat << EOF
export PATH="$REPO/bin:\$PATH"
unalias y 2>/dev/null
y() {
    command y "\$@"
    local last_dir
    last_dir="\$(command y path-list --lastdir-file 2>/dev/null)"
    [ -z "\$last_dir" ] || [ ! -f "\$last_dir" ] || {
        . "\$last_dir"
        rm -f -- "\$last_dir" > /dev/null
    }
}
EOF
    fi
    echo "$BLOCK_END"
}

# without_marmota prints a shell config without what Marmota put in it, now
# or under its old name: the y block, the old cd-on-quit block (from its
# marker to its alias y line, or to the end of its function), and aliases of
# y or of the old binary's name to the old binary.
without_marmota() {
    # The old block is about 30 lines: an end further than 40 lines on isn't its.
    awk -v begin="$BLOCK_BEGIN" -v end="$BLOCK_END" -v lbegin="$LEGACY_BEGIN" -v lend="$LEGACY_END" \
        -v old="$OLD_MARKER" -v alias_re="$OLD_ALIAS" '
        NR == FNR {
            if (index($0, old) && !start) start = FNR
            else if (start && FNR - start <= 40 && !stop_alias && $0 ~ /^alias y[ =]/) stop_alias = FNR
            else if (start && FNR - start <= 40 && !stop_close && $0 ~ /^(}|end)[ \t]*$/) stop_close = FNR
            next
        }
        FNR == 1 { stop = stop_alias ? stop_alias : stop_close }
        start && stop && FNR >= start && FNR <= stop { next }
        start && stop && FNR == start - 1 && $0 ~ /^[ \t]*$/ { next }
        index($0, begin) || index($0, lbegin) { inblock = 1; next }
        inblock { if (index($0, end) || index($0, lend)) inblock = 0; next }
        $0 ~ alias_re { next }
        # Blank lines are held until a line follows: the ones at the end go.
        /^[ \t]*$/ { held = held $0 "\n"; next }
        { printf "%s", held; held = ""; print }
    ' "$1" "$1"
}

# old_binaries prints the binaries an install under the old name used: the
# ones its shell config named, and where it put them.
old_binaries() {
    local f
    for f in "${SHELL_CONFIGS[@]}"; do
        [[ -f $f ]] || continue
        grep -hoE "$OLD_BINARY_VAR"'[ =]"[^"]*"' "$f" | sed -E "s/^$OLD_BINARY_VAR[ =]\"//; s/\"\$//"
        grep -hoE "$OLD_ALIAS" "$f" | grep -oE "[^'\" =]*$OLD_NAME['\"]?$" | tr -d "'\""
    done | sed "s|^\\\$HOME|$HOME|; s|^~|$HOME|"
    echo "$REPO/bin/$OLD_NAME"
    echo "$HOME/.local/bin/$OLD_NAME"
    echo "/usr/local/bin/$OLD_NAME"
}

# remove_old_binaries deletes those that say they are the old program, saying
# what it did.
remove_old_binaries() {
    local f
    while IFS= read -r f; do
        [[ -f $f ]] || continue
        "$f" --version 2>/dev/null | grep -q "$OLD_VERSION_LINE" || continue
        if rm -f "$f" 2>/dev/null; then
            echo "Removed the old binary $f (it is y now)"
        else
            echo "An old binary is still at $f: remove it with  sudo rm '$f'"
        fi
    done < <(old_binaries | awk '!seen[$0]++')
}

# setup_shell puts the y block in each shell config that ran Marmota, or in
# your shell's when none did, taking out what an older install put there. A
# config it changes is backed up next to it first.
setup_shell() {
    local configs=() f
    for f in "${SHELL_CONFIGS[@]}"; do
        if grep -qsF -e "$BLOCK_BEGIN" -e "$LEGACY_BEGIN" -e "$OLD_MARKER" "$f" || grep -qsE "$OLD_ALIAS" "$f"; then
            configs+=("$f")
        fi
    done
    if [[ ${#configs[@]} -eq 0 ]]; then
        case "$SHELL" in
            */fish) configs=("$HOME/.config/fish/config.fish") ;;
            */bash) configs=("$HOME/.bashrc") ;;
            *) configs=("$HOME/.zshrc") ;;
        esac
    fi
    for f in "${configs[@]}"; do
        local existed=""
        [[ -s $f ]] && existed=1
        mkdir -p "$(dirname "$f")"
        touch "$f"
        local kind=sh
        [[ $f == *.fish ]] && kind=fish
        local new
        new="$(mktemp)"
        { without_marmota "$f"; echo; y_block "$kind"; } > "$new"
        if cmp -s "$new" "$f"; then
            rm -f "$new"
            continue
        fi
        local note=""
        if [[ -n $existed ]]; then
            local backup="$f.bak-$(date +%Y%m%d-%H%M%S)"
            cp "$f" "$backup"
            note=" (the old one: $backup)"
        fi
        cat "$new" > "$f"
        rm -f "$new"
        echo "Set up y in $f$note"
        SHELL_CHANGED=1
    done
}

mkdir -p ./bin

# Check and install runtime dependencies
install_runtime_deps() {
    # Check for rsvg-convert (SVG preview)
    if ! command -v rsvg-convert &>/dev/null; then
        echo "Installing librsvg (for SVG preview)..."
        if command -v brew &>/dev/null; then
            brew install librsvg
        else
            echo "Warning: rsvg-convert not found and Homebrew not available."
            echo "Install librsvg manually for SVG preview support."
            echo "  macOS: brew install librsvg"
            echo "  Ubuntu/Debian: sudo apt install librsvg2-bin"
            echo "  Fedora: sudo dnf install librsvg2-tools"
        fi
    fi
}

install_runtime_deps

echo "Building y (Marmota)..."
if ! go build -o "$TARGET"; then
    echo "Build failed!"
    exit 1
fi
echo "Built: $TARGET"

# The shelf Y puts files on (bin/y-shelf), built when Xcode's command line
# tools are here (without them /usr/bin/swiftc only offers to install them)
build_shelf() {
    local src="$REPO/scripts/shelf/main.swift" out="$REPO/bin/y-shelf"
    [[ $(uname -s) == "Darwin" ]] || return 0
    if ! xcode-select -p &>/dev/null; then
        echo "Skipped the shelf (Y): it needs Xcode's command line tools (xcode-select --install)"
        return 0
    fi
    # Built already, and it runs: asked for a file that isn't there, it says
    # so and ends with 1. One macOS won't run (it kills a program whose code
    # signature it takes for invalid, with no word in the terminal) ends
    # otherwise, and is built again.
    if [[ -x $out && $out -nt $src ]]; then
        "$out" "$REPO/bin/.no-such-file" &>/dev/null
        [[ $? -eq 1 ]] && return 0
        echo "The shelf (y-shelf) doesn't run: building it again"
    fi
    echo "Building the shelf (y-shelf)..."
    # Beside it, then moved over it: a signed program written over in place
    # is one macOS won't run.
    if swiftc -O "$src" -o "$out.new"; then
        mv -f "$out.new" "$out"
    else
        rm -f "$out.new"
        echo "Warning: the shelf didn't build, so Y can't open it"
    fi
}
build_shelf

remove_old_binaries
setup_shell

# The skill that tells Claude Code (and Codex, when it is set up) about
# `y session`: starting and following sessions here or on other computers
install_session_skill() {
    local src="$REPO/scripts/skills/marmota-sessions/SKILL.md" dir
    for dir in "$HOME/.claude/skills" "$HOME/.codex/skills"; do
        [[ -d $(dirname "$dir") ]] || continue
        mkdir -p "$dir/marmota-sessions"
        cp "$src" "$dir/marmota-sessions/SKILL.md"
    done
}
install_session_skill

# Claude Code's look, on a computer that has Claude Code: the settings of
# scripts/claude/settings.json (its theme) and the status line
# scripts/claude/statusline.sh, in Claude Code's settings.json. A setting you
# already have stays as you set it. So does a status line of your own, unless
# it is the script Marmota's began as (known by the checksum of its lines of
# code): that one gives way to Marmota's. The status line is named where it
# is in this repository, not copied: a change to it shows in git, and is on
# your other computers with the next pull. The settings as they were are kept
# beside the file.
setup_claude_look() {
    local dir="${CLAUDE_CONFIG_DIR:-$HOME/.claude}"
    local look="$REPO/scripts/claude/settings.json" line="$REPO/scripts/claude/statusline.sh"
    [[ -d $dir ]] || command -v claude &>/dev/null || return 0
    if ! command -v jq &>/dev/null && command -v brew &>/dev/null; then
        echo "Installing jq (Claude Code's status line reads the session with it)..."
        brew install jq
    fi
    if ! command -v jq &>/dev/null; then
        echo "Skipped Claude Code's look: it needs jq (brew install jq, or apt install jq)"
        return 0
    fi

    local settings="$dir/settings.json" was="{}"
    [[ -s $settings ]] && was="$(cat "$settings")"
    if ! jq -e 'type == "object"' <<< "$was" &>/dev/null; then
        echo "Left Claude Code's look as it is: jq can't read $settings"
        return 0
    fi

    # Their status line, when its command is one file (~ for home): is it the
    # script Marmota's began as, comments and empty lines aside?
    local theirs began=false
    theirs="$(jq -r '.statusLine.command? // empty' <<< "$was")"
    theirs="${theirs/#\~\//$HOME/}"
    if [[ -f $theirs && $(grep -vE '^[[:space:]]*(#|$)' "$theirs" | cksum) == "2136977005 1944" ]]; then
        began=true
    fi

    local command now what
    command="$(printf '%q' "$line")"
    now="$(jq --arg command "$command" --argjson began "$began" --slurpfile look "$look" '
        . as $yours
        | . + ($look[0] | with_entries(select(.key as $key | $yours | has($key) | not)))
        | if has("statusLine") | not then
            .statusLine = {type: "command", command: $command}
          elif $began or ((.statusLine.command? // "") | tostring | endswith("/scripts/claude/statusline.sh")) then
            .statusLine.command = $command
          else . end
    ' <<< "$was")" || return 0
    # What changed: the settings added, and a status line that was there
    what="$(jq -r --argjson was "$was" '
        [keys_unsorted[] | select(. as $key | $was | has($key) | not)]
        + (if ($was | has("statusLine")) and .statusLine != $was.statusLine then ["statusLine"] else [] end)
        | join(", ")
    ' <<< "$now")"
    if [[ -n $what ]]; then
        local note=""
        mkdir -p "$dir"
        if [[ -s $settings ]]; then
            local backup="$settings.bak-$(date +%Y%m%d-%H%M%S)"
            cp -p "$settings" "$backup"
            note=" (as it was: $backup)"
        fi
        (umask 077 && printf '%s\n' "$now" > "$settings") || return 0
        echo "Set up Claude Code's look in $settings: $what$note"
    fi
    if ! jq -e --arg command "$command" '.statusLine.command? == $command' <<< "$now" &>/dev/null; then
        echo "Claude Code's status line is yours, as you set it: Marmota's is $line"
    fi
}
setup_claude_look

# Update user config with missing fields
echo "Updating user config with missing fields..."
"$TARGET" --fix-config-file --fix-only >/dev/null 2>&1 || true
echo "User config updated"

# On a Mac, setup-macos-hotkeys.sh sets up every key: Marmota's own, brought
# up to date in your hotkeys.toml (an old default gets today's, a key you
# chose stays), micro as Marmota's editor (its tools, settings, keys and
# plugins, and editor = "micro" in Marmota's config) and Marmota's block in
# ~/.wezterm.lua: its look, and the Mac keys it sends micro. Brought up to
# date on each build, so a computer is ready once Marmota is built on it.
# Elsewhere there is only Marmota's own keys to bring up to date.
if [[ $(uname -s) == "Darwin" ]]; then
    ./setup-macos-hotkeys.sh --micro --no-color 2>&1 | grep -i -E "micro|Marmota's|opens files" || true
else
    "$TARGET" --fix-hotkeys --fix-only >/dev/null 2>&1 || true
fi

if [[ -n "$SHELL_CHANGED" ]]; then
    echo "Open a new terminal (or source your shell config) to run y"
fi
