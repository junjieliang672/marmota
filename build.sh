#!/usr/bin/env bash

# Builds y (Marmota) into ./bin/y, and sets up your shell to run it: a block
# in your shell config puts ./bin on PATH and defines the y function, which
# moves your shell to the folder y quit in when cd_on_quit is on. On a Mac it
# also keeps Marmota's look in ~/.wezterm.lua up to date. An install
# from before the binary was called y (spf, with y an alias) is moved over:
# its shell lines and its spf binaries go. On a Mac where Marmota was never
# installed (no Go, or nothing in your shell config), it first installs
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
        sed -n '3,18p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
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
# The same block, as it was marked before the program was Marmota
LEGACY_BEGIN="# >>> y (superfile) >>>"
LEGACY_END="# <<< y (superfile) <<<"
# What an install from before y wrote: this block, which ended in alias y='spf'
OLD_MARKER="Superfile cd-on-quit wrapper"
# ... or an alias of y or spf to a superfile binary
OLD_ALIAS='^alias (y|spf)[ =].*spf'

# installed_before reports a shell config that runs superfile, old or new.
installed_before() {
    grep -qsF -e "$BLOCK_BEGIN" -e "$LEGACY_BEGIN" -e "$OLD_MARKER" "${SHELL_CONFIGS[@]}" ||
        grep -qsE "$OLD_ALIAS" "${SHELL_CONFIGS[@]}"
}

# Never installed: no Go, or no shell config that runs superfile
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

# without_superfile prints a shell config without what superfile put in it:
# the y block, the old cd-on-quit block (from its marker to its alias y line,
# or to the end of its function), and aliases of y or spf to superfile.
without_superfile() {
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

# old_binaries prints the spf binaries an older install used: the ones its
# shell config named, and where it put them.
old_binaries() {
    local f
    for f in "${SHELL_CONFIGS[@]}"; do
        [[ -f $f ]] || continue
        grep -hoE 'SPF_BINARY[ =]"[^"]*"' "$f" | sed -E 's/^SPF_BINARY[ =]"//; s/"$//'
        grep -hoE "$OLD_ALIAS" "$f" | grep -oE "[^'\" =]*spf['\"]?$" | tr -d "'\""
    done | sed "s|^\\\$HOME|$HOME|; s|^~|$HOME|"
    echo "$REPO/bin/spf"
    echo "$HOME/.local/bin/spf"
    echo "/usr/local/bin/spf"
}

# remove_old_binaries deletes those that are superfile, saying what it did.
remove_old_binaries() {
    local f
    while IFS= read -r f; do
        [[ -f $f ]] || continue
        "$f" --version 2>/dev/null | grep -q '^superfile version' || continue
        if rm -f "$f" 2>/dev/null; then
            echo "Removed the old superfile binary $f (it is y now)"
        else
            echo "An old superfile binary is still at $f: remove it with  sudo rm '$f'"
        fi
    done < <(old_binaries | awk '!seen[$0]++')
}

# setup_shell puts the y block in each shell config that ran superfile, or in
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
        { without_superfile "$f"; echo; y_block "$kind"; } > "$new"
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

remove_old_binaries
setup_shell

# Marmota's WezTerm look: the active pane bright, blue lines between panes, a
# blue edge beside the active pane (a block in ~/.wezterm.lua, kept current)
if [[ $(uname -s) == "Darwin" && -f "$HOME/.wezterm.lua" ]]; then
    ./setup-macos-hotkeys.sh --wezterm-look --no-color 2>&1 | grep "Marmota's" || true
fi

# Update user config with missing fields
echo "Updating user config with missing fields..."
"$TARGET" --fix-config-file --fix-hotkeys --fix-only >/dev/null 2>&1 || true
echo "User config updated"
if [[ -n "$SHELL_CHANGED" ]]; then
    echo "Open a new terminal (or source your shell config) to run y"
fi
