#!/bin/bash

# =============================================================================
# macOS Hotkey Setup Script
# =============================================================================
# This script configures macOS-style hotkeys for WezTerm, Neovide/Neovim,
# and Marmota. It can be run on any Mac to apply consistent hotkey settings.
#
# Usage: ./setup-macos-hotkeys.sh [OPTIONS]
# See --help for available options
# =============================================================================

# Color definitions
green='\033[0;32m'
red='\033[0;31m'
yellow='\033[0;33m'
cyan='\033[0;36m'
bright_green='\033[1;32m'
bright_yellow='\033[1;33m'
bright_cyan='\033[1;36m'
bright_red='\033[1;31m'
nc='\033[0m' # No Color

# Script flags
DRY_RUN=false
CONFIGURE_WEZTERM=true
CONFIGURE_NEOVIM=true
CONFIGURE_SUPERFILE=true
PATCH_CMD_K=false
WEZTERM_LOOK=false
NO_COLOR=false

# =============================================================================
# Helper Functions
# =============================================================================

print_banner() {
    if [[ $NO_COLOR == false ]]; then
        echo -e "${bright_cyan}"
        cat << 'EOF'
+----------------------------------------------------------+
|                                                          |
|   macOS Hotkey Setup Script                              |
|                                                          |
+----------------------------------------------------------+
EOF
        echo -e "${nc}"
    else
        echo "macOS Hotkey Setup Script"
        echo "========================="
    fi
}

print_help() {
    cat << 'EOF'
Usage: ./setup-macos-hotkeys.sh [OPTIONS]

Options:
  --dry-run         Show what would be done without making changes
  --wezterm         Configure WezTerm only
  --neovim          Configure Neovim only
  --marmota       Configure Marmota only
  --cmd-k           Only make Cmd+K leave Marmota alone, in your existing
                    ~/.wezterm.lua (everything else in it stays)
  --wezterm-look    Only add or update Marmota's block in ~/.wezterm.lua: the
                    active pane bright, blue lines between panes, a blue edge
                    beside the active pane (./build.sh runs this)
  --all             Configure everything (default)
  --no-color        Disable colored output
  -h, --help        Show this help message

Description:
  This script sets up macOS-style hotkeys for:
  - WezTerm: Cmd+Arrow navigation, Opt+Arrow word movement, etc.
  - Neovim: gy to yank an @file#L12-15 reference (what Claude Code attaches) instead of the text; gY absolute
  - Marmota: Sets nvim as default editor

  Existing configs are backed up with timestamps before being overwritten.

Examples:
  ./setup-macos-hotkeys.sh              # Configure everything
  ./setup-macos-hotkeys.sh --dry-run    # Preview changes
  ./setup-macos-hotkeys.sh --wezterm    # Only configure WezTerm
  ./setup-macos-hotkeys.sh --neovim     # Only configure Neovim
  ./setup-macos-hotkeys.sh --cmd-k      # Only change Cmd+K in ~/.wezterm.lua

EOF
}

log_info() {
    if [[ $NO_COLOR == false ]]; then
        echo -e "${bright_cyan}>${nc} $1"
    else
        echo "> $1"
    fi
}

log_success() {
    if [[ $NO_COLOR == false ]]; then
        echo -e "${bright_green}+${nc} $1"
    else
        echo "+ $1"
    fi
}

log_warning() {
    if [[ $NO_COLOR == false ]]; then
        echo -e "${bright_yellow}!${nc} $1"
    else
        echo "! $1"
    fi
}

log_error() {
    if [[ $NO_COLOR == false ]]; then
        echo -e "${bright_red}x${nc} $1"
    else
        echo "x $1"
    fi
}

# =============================================================================
# Argument Parsing
# =============================================================================

parse_args() {
    # Check if specific components are requested
    local specific_requested=false

    while [[ $# -gt 0 ]]; do
        case $1 in
            --dry-run)
                DRY_RUN=true
                shift
                ;;
            --wezterm)
                if [[ $specific_requested == false ]]; then
                    CONFIGURE_WEZTERM=false
                    CONFIGURE_NEOVIM=false
                    CONFIGURE_SUPERFILE=false
                    specific_requested=true
                fi
                CONFIGURE_WEZTERM=true
                shift
                ;;
            --neovim)
                if [[ $specific_requested == false ]]; then
                    CONFIGURE_WEZTERM=false
                    CONFIGURE_NEOVIM=false
                    CONFIGURE_SUPERFILE=false
                    specific_requested=true
                fi
                CONFIGURE_NEOVIM=true
                shift
                ;;
            --marmota)
                if [[ $specific_requested == false ]]; then
                    CONFIGURE_WEZTERM=false
                    CONFIGURE_NEOVIM=false
                    CONFIGURE_SUPERFILE=false
                    specific_requested=true
                fi
                CONFIGURE_SUPERFILE=true
                shift
                ;;
            --cmd-k)
                if [[ $specific_requested == false ]]; then
                    CONFIGURE_WEZTERM=false
                    CONFIGURE_NEOVIM=false
                    CONFIGURE_SUPERFILE=false
                    specific_requested=true
                fi
                PATCH_CMD_K=true
                shift
                ;;
            --wezterm-look)
                if [[ $specific_requested == false ]]; then
                    CONFIGURE_WEZTERM=false
                    CONFIGURE_NEOVIM=false
                    CONFIGURE_SUPERFILE=false
                    specific_requested=true
                fi
                WEZTERM_LOOK=true
                shift
                ;;
            --all)
                CONFIGURE_WEZTERM=true
                CONFIGURE_NEOVIM=true
                CONFIGURE_SUPERFILE=true
                shift
                ;;
            --no-color)
                NO_COLOR=true
                shift
                ;;
            -h|--help)
                print_help
                exit 0
                ;;
            *)
                log_error "Unknown option: $1"
                echo "Use --help for usage information"
                exit 1
                ;;
        esac
    done
}

# =============================================================================
# Backup Function
# =============================================================================

backup_file() {
    local file="$1"
    if [[ -f "$file" ]]; then
        local timestamp=$(date +%Y%m%d-%H%M%S)
        local backup="${file}.backup.${timestamp}"
        if [[ $DRY_RUN == true ]]; then
            log_info "[DRY RUN] Would backup: $file -> $backup"
        else
            cp "$file" "$backup"
            log_success "Backed up: $file -> $backup"
        fi
    fi
}

# =============================================================================
# WezTerm Configuration
# =============================================================================

# wezterm_cmd_k_block prints Cmd+K's entry in config.keys. Cmd+K clears the
# scrollback and the screen without telling the program in the pane, and
# Marmota, which redraws only what changes, then stayed blank: in Marmota
# it does nothing.
wezterm_cmd_k_block() {
    cat << 'CMD_K_EOF'
  -- Clear scrollback, but not in Marmota: it can't tell the screen was
  -- cleared, and would stay blank until something redrew it
  {
    key = 'k',
    mods = 'CMD',
    action = wezterm.action_callback(function(window, pane)
      local program = (pane:get_foreground_process_name() or ''):match('[^/\\]+$')
      if program == 'y' or program == 'spf' or program == 'superfile' then
        return
      end
      window:perform_action(wezterm.action.ClearScrollback 'ScrollbackAndViewport', pane)
    end),
  },
CMD_K_EOF
}

# patch_wezterm_cmd_k puts that entry in place of Cmd+K's in an existing
# ~/.wezterm.lua, and leaves the rest of it as it is.
patch_wezterm_cmd_k() {
    if [[ $PATCH_CMD_K == false ]]; then
        return
    fi

    echo ""
    log_info "Making Cmd+K leave Marmota alone..."

    local wezterm_config="$HOME/.wezterm.lua"
    if [[ ! -f "$wezterm_config" ]]; then
        log_warning "There is no $wezterm_config: --wezterm writes one with it"
        return
    fi
    if grep -q "program == 'y'" "$wezterm_config"; then
        log_success "Cmd+K already leaves Marmota alone"
        return
    fi
    if grep -q "program == 'spf'" "$wezterm_config"; then
        # Set up before the binary was called y: Marmota runs as y now
        [[ $DRY_RUN == true ]] && { log_info "[DRY RUN] Would add y to Cmd+K's check"; return; }
        backup_file "$wezterm_config"
        sed -i '' "s/if program == 'spf' or/if program == 'y' or program == 'spf' or/" "$wezterm_config"
        log_success "Cmd+K leaves Marmota alone as y too"
        return
    fi
    if ! grep -q "ClearScrollback 'ScrollbackAndViewport'" "$wezterm_config"; then
        log_warning "Cmd+K has no entry in $wezterm_config to change; add this to config.keys:"
        wezterm_cmd_k_block
        return
    fi
    if [[ $DRY_RUN == true ]]; then
        log_info "[DRY RUN] Would change Cmd+K's entry in $wezterm_config"
        return
    fi

    backup_file "$wezterm_config"
    SPF_CMD_K="$(wezterm_cmd_k_block)" perl -0777 -pi -e \
        's/(?:[ \t]*-- Clear scrollback[^\n]*\n)?[ \t]*\{\s*key = .k.,\s*mods = .CMD.,\s*action = wezterm\.action\.ClearScrollback .ScrollbackAndViewport.,\s*\},[ \t]*\n/$ENV{SPF_CMD_K}\n/' \
        "$wezterm_config"
    if grep -q "program == 'y'" "$wezterm_config"; then
        log_success "Cmd+K leaves Marmota alone now; WezTerm reloads its config by itself"
    else
        log_error "Couldn't change Cmd+K's entry in $wezterm_config; put this in its place:"
        wezterm_cmd_k_block
    fi
}

configure_wezterm() {
    if [[ $CONFIGURE_WEZTERM == false ]]; then
        return
    fi

    echo ""
    log_info "Configuring WezTerm..."

    local wezterm_config="$HOME/.wezterm.lua"

    # Backup existing config
    backup_file "$wezterm_config"

    if [[ $DRY_RUN == true ]]; then
        log_info "[DRY RUN] Would create: $wezterm_config"
        return
    fi

    {
        cat << 'WEZTERM_EOF'
local wezterm = require 'wezterm'
local config = {}

-- Use config builder if available (WezTerm 20220807 and later)
if wezterm.config_builder then
  config = wezterm.config_builder()
end

-- ============================================
-- Appearance
-- ============================================

-- Color scheme
config.color_scheme = 'Tokyo Night'

-- Font configuration
config.font = wezterm.font('JetBrains Mono', { weight = 'Regular' })
config.font_size = 14.0

-- Window appearance
config.window_decorations = 'RESIZE'
config.window_background_opacity = 0.95
config.macos_window_background_blur = 20

-- Tab bar
config.enable_tab_bar = true
config.hide_tab_bar_if_only_one_tab = true
config.use_fancy_tab_bar = false
config.tab_bar_at_bottom = false

-- ============================================
-- Key Bindings
-- ============================================

config.keys = {
  -- Tab management
  {
    key = 't',
    mods = 'CMD',
    action = wezterm.action.SpawnTab 'CurrentPaneDomain',
  },
  {
    key = 'w',
    mods = 'CMD',
    action = wezterm.action.CloseCurrentTab { confirm = true },
  },
  {
    key = '[',
    mods = 'CMD',
    action = wezterm.action.ActivateTabRelative(-1),
  },
  {
    key = ']',
    mods = 'CMD',
    action = wezterm.action.ActivateTabRelative(1),
  },

  -- Pane splitting
  {
    key = 'd',
    mods = 'CMD',
    action = wezterm.action.SplitHorizontal { domain = 'CurrentPaneDomain' },
  },
  {
    key = 'd',
    mods = 'CMD|SHIFT',
    action = wezterm.action.SplitVertical { domain = 'CurrentPaneDomain' },
  },

  -- Pane navigation
  {
    key = 'LeftArrow',
    mods = 'CMD|OPT',
    action = wezterm.action.ActivatePaneDirection 'Left',
  },
  {
    key = 'RightArrow',
    mods = 'CMD|OPT',
    action = wezterm.action.ActivatePaneDirection 'Right',
  },
  {
    key = 'UpArrow',
    mods = 'CMD|OPT',
    action = wezterm.action.ActivatePaneDirection 'Up',
  },
  {
    key = 'DownArrow',
    mods = 'CMD|OPT',
    action = wezterm.action.ActivatePaneDirection 'Down',
  },

  -- Pane resizing
  {
    key = 'LeftArrow',
    mods = 'CMD|SHIFT',
    action = wezterm.action.AdjustPaneSize { 'Left', 5 },
  },
  {
    key = 'RightArrow',
    mods = 'CMD|SHIFT',
    action = wezterm.action.AdjustPaneSize { 'Right', 5 },
  },
  {
    key = 'UpArrow',
    mods = 'CMD|SHIFT',
    action = wezterm.action.AdjustPaneSize { 'Up', 5 },
  },
  {
    key = 'DownArrow',
    mods = 'CMD|SHIFT',
    action = wezterm.action.AdjustPaneSize { 'Down', 5 },
  },

  -- Close current pane
  {
    key = 'w',
    mods = 'CMD|SHIFT',
    action = wezterm.action.CloseCurrentPane { confirm = true },
  },

WEZTERM_EOF
        wezterm_cmd_k_block
        cat << 'WEZTERM_EOF'

  -- Toggle fullscreen
  {
    key = 'Enter',
    mods = 'CMD|SHIFT',
    action = wezterm.action.ToggleFullScreen,
  },

  -- Copy/Paste
  {
    key = 'c',
    mods = 'CMD',
    action = wezterm.action.CopyTo 'Clipboard',
  },
  {
    key = 'v',
    mods = 'CMD',
    action = wezterm.action.PasteFrom 'Clipboard',
  },

  -- Search
  {
    key = 'f',
    mods = 'CMD',
    action = wezterm.action.Search 'CurrentSelectionOrEmptyString',
  },

  -- Activate command palette
  {
    key = 'p',
    mods = 'CMD|SHIFT',
    action = wezterm.action.ActivateCommandPalette,
  },

  -- Quick select URLs/paths
  {
    key = 'u',
    mods = 'CMD|SHIFT',
    action = wezterm.action.QuickSelectArgs {
      patterns = {
        'https?://\\S+',
        'file://\\S+',
        '\\S+\\.\\S+',
      },
    },
  },

  -- Switch to specific tabs (1-9)
  {
    key = '1',
    mods = 'CMD',
    action = wezterm.action.ActivateTab(0),
  },
  {
    key = '2',
    mods = 'CMD',
    action = wezterm.action.ActivateTab(1),
  },
  {
    key = '3',
    mods = 'CMD',
    action = wezterm.action.ActivateTab(2),
  },
  {
    key = '4',
    mods = 'CMD',
    action = wezterm.action.ActivateTab(3),
  },
  {
    key = '5',
    mods = 'CMD',
    action = wezterm.action.ActivateTab(4),
  },
  {
    key = '6',
    mods = 'CMD',
    action = wezterm.action.ActivateTab(5),
  },
  {
    key = '7',
    mods = 'CMD',
    action = wezterm.action.ActivateTab(6),
  },
  {
    key = '8',
    mods = 'CMD',
    action = wezterm.action.ActivateTab(7),
  },
  {
    key = '9',
    mods = 'CMD',
    action = wezterm.action.ActivateTab(8),
  },

  -- Option + Arrow: Word movement (macOS style)
  {
    key = 'LeftArrow',
    mods = 'OPT',
    action = wezterm.action.SendString '\x1bb', -- backward-word
  },
  {
    key = 'RightArrow',
    mods = 'OPT',
    action = wezterm.action.SendString '\x1bf', -- forward-word
  },

  -- Command + Arrow: Line start/end (macOS style)
  {
    key = 'LeftArrow',
    mods = 'CMD',
    action = wezterm.action.SendString '\x01', -- Ctrl-A, beginning-of-line
  },
  {
    key = 'RightArrow',
    mods = 'CMD',
    action = wezterm.action.SendString '\x05', -- Ctrl-E, end-of-line
  },

  -- Option + Backspace: Delete previous word
  {
    key = 'Backspace',
    mods = 'OPT',
    action = wezterm.action.SendString '\x17', -- Ctrl-W
  },

  -- Command + Backspace: Delete to line start
  {
    key = 'Backspace',
    mods = 'CMD',
    action = wezterm.action.SendString '\x15', -- Ctrl-U
  },

  {
    key = 'Enter',
    mods = 'SHIFT',
    action = wezterm.action.SendString '\n',
  },
}

-- ============================================
-- Mouse Bindings
-- ============================================

config.mouse_bindings = {
  -- Open links with CMD+click
  {
    event = { Up = { streak = 1, button = 'Left' } },
    mods = 'CMD',
    action = wezterm.action.OpenLinkAtMouseCursor,
  },
}

-- ============================================
-- Performance
-- ============================================

config.max_fps = 120
config.animation_fps = 60
config.cursor_blink_rate = 800

-- ============================================
-- Other Settings
-- ============================================

-- Scrollback
config.scrollback_lines = 10000

return config
WEZTERM_EOF
    } > "$wezterm_config"

    if [[ $? -eq 0 ]]; then
        log_success "WezTerm configuration created at $wezterm_config"
    else
        log_error "Failed to create WezTerm configuration"
    fi
}

# =============================================================================
# Neovim Configuration
# =============================================================================
# macOS Command-key hotkeys are not configured: Neovide has been removed and
# terminal neovim cannot capture Command combinations anyway. What we do install
# is the spf_yank_ref module, so files opened from superfile with `e` can yank an
# @file#L12-15 reference, which Claude Code attaches when pasted, instead of the
# selected text.
# =============================================================================

configure_neovim() {
    if [[ $CONFIGURE_NEOVIM == false ]]; then
        return
    fi

    echo ""
    log_info "Configuring Neovim..."

    local script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    local source_module="$script_dir/scripts/nvim/spf_yank_ref.lua"
    local nvim_lua_dir="$HOME/.config/nvim/lua"
    local target_module="$nvim_lua_dir/spf_yank_ref.lua"

    if [[ ! -f "$source_module" ]]; then
        log_error "Missing $source_module - skipping spf_yank_ref install"
        return
    fi

    if [[ ! -d "$nvim_lua_dir" ]]; then
        log_warning "No $nvim_lua_dir found - skipping spf_yank_ref install"
        log_info "Install a neovim config (e.g. NvChad) and re-run with --neovim"
        return
    fi

    # Install the module itself
    if cmp -s "$source_module" "$target_module"; then
        log_success "spf_yank_ref.lua already up to date"
    else
        backup_file "$target_module"
        if [[ $DRY_RUN == true ]]; then
            log_info "[DRY RUN] Would copy: $source_module -> $target_module"
        else
            cp "$source_module" "$target_module"
            log_success "Installed $target_module"
        fi
    fi

    # Make sure something requires it. NvChad keeps user mappings in
    # lua/mappings.lua; otherwise fall back to init.lua.
    local require_line='require "spf_yank_ref"'
    local loader="$nvim_lua_dir/mappings.lua"
    if [[ ! -f "$loader" ]]; then
        loader="$HOME/.config/nvim/init.lua"
    fi

    if [[ ! -f "$loader" ]]; then
        log_warning "No mappings.lua or init.lua found - add this line yourself:"
        echo "      $require_line"
    elif grep -q "spf_yank_ref" "$loader"; then
        log_success "$loader already loads spf_yank_ref"
    elif [[ $DRY_RUN == true ]]; then
        log_info "[DRY RUN] Would append '$require_line' to $loader"
    else
        backup_file "$loader"
        printf '\n-- yank @file#L12-15 references (gy / gY / <leader>yf)\n%s\n' \
            "$require_line" >> "$loader"
        log_success "Appended '$require_line' to $loader"
    fi

    log_info "Mappings: gy @repo/relative#L12, gY @/abs/path#L12, <leader>yl / <leader>yL same, <leader>yf path only"
}

# =============================================================================
# Marmota Configuration
# =============================================================================

configure_superfile() {
    if [[ $CONFIGURE_SUPERFILE == false ]]; then
        return
    fi

    echo ""
    log_info "Configuring Marmota..."

    local spf_config_dir="$HOME/.config/marmota"
    local spf_config_file="$spf_config_dir/config.toml"
    local script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    local default_config="$script_dir/src/superfile_config/config.toml"

    # Ensure directory exists
    if [[ $DRY_RUN == false ]]; then
        mkdir -p "$spf_config_dir"
    fi

    # If config doesn't exist or is incomplete, copy the full default config
    if [[ ! -f "$spf_config_file" ]] || ! grep -q "^border_top" "$spf_config_file" 2>/dev/null; then
        if [[ -f "$default_config" ]]; then
            if [[ $DRY_RUN == true ]]; then
                log_info "[DRY RUN] Would copy default config to $spf_config_file"
            else
                # Backup existing config if it exists
                if [[ -f "$spf_config_file" ]]; then
                    local backup_file="$spf_config_file.backup.$(date +%Y%m%d-%H%M%S)"
                    cp "$spf_config_file" "$backup_file"
                    log_success "Backed up existing config to $backup_file"
                fi
                # Copy the full default config (which already has editor=nvim and zoxide_support=true)
                cp "$default_config" "$spf_config_file"
                # Update second_editor to open nvim in new WezTerm tab
                if [[ "$(uname)" == "Darwin" ]]; then
                    sed -i '' 's/^second_editor.*=.*/second_editor = "wezterm cli spawn -- nvim"/' "$spf_config_file"
                else
                    sed -i 's/^second_editor.*=.*/second_editor = "wezterm cli spawn -- nvim"/' "$spf_config_file"
                fi
                log_success "Copied full default config to $spf_config_file"
            fi
        else
            log_warning "Default config not found at $default_config"
            # Fallback: just set editor if we can't find default config
            if [[ $DRY_RUN == false ]]; then
                if [[ ! -f "$spf_config_file" ]]; then
                    cat > "$spf_config_file" << 'MINIMAL_CONFIG'
editor = "nvim"
second_editor = "wezterm cli spawn -- nvim"
zoxide_support = true
MINIMAL_CONFIG
                    log_success "Created minimal config with editor=nvim, second_editor opens nvim in new tab"
                fi
            fi
        fi
    else
        # Config exists and appears complete, just update editor and zoxide
        if [[ $DRY_RUN == true ]]; then
            log_info "[DRY RUN] Would update editor settings in $spf_config_file"
        else
            # Update editor lines - e opens nvim, E opens nvim in new WezTerm tab
            if [[ "$(uname)" == "Darwin" ]]; then
                sed -i '' 's/^editor.*=.*/editor = "nvim"/' "$spf_config_file"
                # Add second_editor if it doesn't exist
                if ! grep -q "^second_editor" "$spf_config_file"; then
                    sed -i '' '/^editor.*=/a\
second_editor = "wezterm cli spawn -- nvim"
' "$spf_config_file"
                else
                    sed -i '' 's/^second_editor.*=.*/second_editor = "wezterm cli spawn -- nvim"/' "$spf_config_file"
                fi
                sed -i '' 's/^zoxide_support.*=.*/zoxide_support = true/' "$spf_config_file"
            else
                sed -i 's/^editor.*=.*/editor = "nvim"/' "$spf_config_file"
                # Add second_editor if it doesn't exist
                if ! grep -q "^second_editor" "$spf_config_file"; then
                    sed -i '/^editor.*=/a second_editor = "wezterm cli spawn -- nvim"' "$spf_config_file"
                else
                    sed -i 's/^second_editor.*=.*/second_editor = "wezterm cli spawn -- nvim"/' "$spf_config_file"
                fi
                sed -i 's/^zoxide_support.*=.*/zoxide_support = true/' "$spf_config_file"
            fi
            log_success "Updated editor=nvim, second_editor opens nvim in new tab, and enabled zoxide in $spf_config_file"
        fi
    fi
}

# =============================================================================
# Summary
# =============================================================================

print_summary() {
    echo ""
    if [[ $NO_COLOR == false ]]; then
        echo -e "${bright_cyan}==========================================================${nc}"
        echo -e "${bright_green}  Setup Complete${nc}"
        echo -e "${bright_cyan}==========================================================${nc}"
    else
        echo "=========================================================="
        echo "  Setup Complete"
        echo "=========================================================="
    fi

    echo ""
    echo "Configured:"
    if [[ $CONFIGURE_WEZTERM == true ]]; then
        echo "  - WezTerm: ~/.wezterm.lua"
    fi
    if [[ $CONFIGURE_NEOVIM == true ]]; then
        echo "  - Neovim: ~/.config/nvim/lua/spf_yank_ref.lua (yank @file#L refs)"
    fi
    if [[ $CONFIGURE_SUPERFILE == true ]]; then
        echo "  - Marmota: ~/.config/marmota/config.toml"
    fi
    if [[ $PATCH_CMD_K == true ]]; then
        echo "  - WezTerm: Cmd+K leaves Marmota alone (~/.wezterm.lua)"
    fi
    if [[ $CONFIGURE_WEZTERM == false && $CONFIGURE_NEOVIM == false && $CONFIGURE_SUPERFILE == false ]]; then
        echo ""
        return
    fi

    echo ""
    echo "Next steps:"
    echo "  1. Restart WezTerm to apply new hotkeys"
    echo "  2. Marmota will now open files with nvim (e or E key)"
    echo "  3. In nvim, select lines and press gy to yank @repo/relative/file#L12-15 (gY: absolute)"
    echo ""
}

# =============================================================================
# Marmota's WezTerm look
# =============================================================================

# wezterm_look_block prints the block Marmota keeps in ~/.wezterm.lua: the
# pane you type in is the bright one, the line between panes is blue on your
# color scheme, and the window's edge beside the active pane turns blue
# (WezTerm can't draw a border around one pane). Your key bindings are left alone.
wezterm_look_block() {
    cat << 'LOOK_EOF'
-- >>> marmota >>>
-- Marmota's look (setup-macos-hotkeys.sh writes this block, ./build.sh keeps
-- it up to date; your settings above it are left as they are)
do
  local wezterm = require 'wezterm'
  local edge_on, edge_off = '#7aa2f7', '#1a1b26'

  -- The line between panes in blue, on your color scheme
  local name = config.color_scheme
  local scheme = name and ((config.color_schemes or {})[name] or wezterm.color.get_builtin_schemes()[name])
  if scheme then
    local blue = {}
    for k, v in pairs(scheme) do
      blue[k] = v
    end
    blue.split = edge_on
    edge_off = blue.background or edge_off
    config.color_schemes = config.color_schemes or {}
    config.color_schemes[name .. ' (blue splits)'] = blue
    config.color_scheme = name .. ' (blue splits)'
  else
    config.colors = config.colors or {}
    config.colors.split = edge_on
  end

  -- The pane you type in is the bright one
  config.inactive_pane_hsb = { saturation = 1.0, brightness = 0.7 }

  -- The window's edge beside the active pane: left or right when panes sit
  -- side by side, top or bottom when stacked, none with one pane
  local base_frame = config.window_frame or {}
  local function frame(edges)
    local f = {}
    for k, v in pairs(base_frame) do
      f[k] = v
    end
    local function color(on)
      if on then
        return edge_on
      end
      return edge_off
    end
    f.border_left_width, f.border_right_width = '0.4cell', '0.4cell'
    f.border_top_height, f.border_bottom_height = '0.2cell', '0.2cell'
    f.border_left_color, f.border_right_color = color(edges.left), color(edges.right)
    f.border_top_color, f.border_bottom_color = color(edges.top), color(edges.bottom)
    return f
  end
  config.window_frame = frame({})
  -- The edge follows the focus within 50 ms, whatever moved it: your pane keys,
  -- a click, Marmota. Your key bindings are left as they are.
  config.status_update_interval = 50

  local shown = {}
  local function update(window)
    local tab = window:active_tab()
    if not tab then
      return
    end
    local panes = tab:panes_with_info()
    local right, bottom, active = 0, 0, nil
    for _, p in ipairs(panes) do
      right = math.max(right, p.left + p.width)
      bottom = math.max(bottom, p.top + p.height)
      if p.is_active then
        active = p
      end
    end
    local edges = {}
    if active and #panes > 1 then
      if active.width < right then
        edges.left = active.left == 0
        edges.right = active.left + active.width >= right
      end
      if active.height < bottom then
        edges.top = active.top == 0
        edges.bottom = active.top + active.height >= bottom
      end
    end
    local key = tostring(edges.left) .. tostring(edges.right) .. tostring(edges.top) .. tostring(edges.bottom)
    local id = window:window_id()
    if shown[id] == key then
      return
    end
    shown[id] = key
    local overrides = window:get_config_overrides() or {}
    overrides.window_frame = frame(edges)
    window:set_config_overrides(overrides)
  end
  wezterm.on('update-status', function(window, _)
    update(window)
  end)
end
-- <<< marmota <<<
LOOK_EOF
}

# ensure_wezterm_look puts Marmota's block in ~/.wezterm.lua, or brings it up
# to date: before the file's last "return config", the old block taken out.
# The file is backed up first when it changes.
ensure_wezterm_look() {
    local wezterm_config="$HOME/.wezterm.lua"
    if [[ ! -f "$wezterm_config" ]]; then
        return
    fi
    if ! grep -q '^return config' "$wezterm_config"; then
        log_warning "$wezterm_config doesn't end in 'return config': Marmota's look isn't added (put this before your return):"
        wezterm_look_block
        return
    fi
    local new
    new="$(mktemp)"
    local block
    block="$(mktemp)"
    wezterm_look_block > "$block"
    awk -v blockfile="$block" '
        NR == FNR { if ($0 ~ /^return config/) last = FNR; next }
        /^-- >>> marmota >>>/ { skip = 1; next }
        skip { if ($0 ~ /^-- <<< marmota <<</) { skip = 0; after = 1 } next }
        # the blank lines after the old block go with it
        after && /^[ \t]*$/ { next }
        { after = 0 }
        FNR == last {
            while ((getline line < blockfile) > 0) print line
            print ""
        }
        { print }
    ' "$wezterm_config" "$wezterm_config" > "$new"
    rm -f "$block"
    if cmp -s "$new" "$wezterm_config"; then
        rm -f "$new"
        log_success "Marmota's WezTerm look is up to date"
        return
    fi
    if [[ $DRY_RUN == true ]]; then
        rm -f "$new"
        log_info "[DRY RUN] Would add Marmota's look to $wezterm_config"
        return
    fi
    backup_file "$wezterm_config"
    cat "$new" > "$wezterm_config"
    rm -f "$new"
    log_success "Marmota's look is in $wezterm_config; WezTerm reloads it by itself"
}

# =============================================================================
# Main
# =============================================================================

main() {
    parse_args "$@"

    print_banner

    if [[ $DRY_RUN == true ]]; then
        log_warning "DRY RUN MODE - No changes will be made"
        echo ""
    fi

    configure_wezterm
    patch_wezterm_cmd_k
    if [[ $CONFIGURE_WEZTERM == true || $PATCH_CMD_K == true || $WEZTERM_LOOK == true ]]; then
        ensure_wezterm_look
    fi
    configure_neovim
    configure_superfile

    if [[ $DRY_RUN == false ]]; then
        print_summary
    else
        echo ""
        log_info "Dry run complete. Run without --dry-run to apply changes."
    fi
}

main "$@"
