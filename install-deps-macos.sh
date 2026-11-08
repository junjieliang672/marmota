#!/bin/bash

# =============================================================================
# macOS Dependency Installation Script for Marmota
# =============================================================================
# This script installs all prerequisites and dependencies needed to build
# and run Marmota on macOS with full functionality.
#
# Usage: ./install-deps-macos.sh [OPTIONS]
# See --help for available options
# =============================================================================

# Color definitions
green='\033[0;32m'
red='\033[0;31m'
yellow='\033[0;33m'
blue='\033[0;34m'
cyan='\033[0;36m'
bright_green='\033[1;32m'
bright_yellow='\033[1;33m'
bright_cyan='\033[1;36m'
bright_red='\033[1;31m'
nc='\033[0m' # No Color

# Installation mode flags
INSTALL_MODE="full"  # minimal, full, or dev
DRY_RUN=false
SKIP_BUILD=false
SKIP_HOMEBREW=false
SKIP_OPTIONAL=false
SKIP_EXIFTOOL=false
SKIP_ZOXIDE=false
SKIP_FFMPEG=false
SKIP_BAT=false
SKIP_MUPDF=false
SKIP_LIBRSVG=false
SKIP_FZF=false
SKIP_WEZTERM=false
SKIP_NERDFONT=false
SKIP_HOTKEYS=false
NO_COLOR=false

# Installation tracking
INSTALLED_CORE=()
INSTALLED_OPTIONAL=()
INSTALLED_DEV=()
SKIPPED=()
FAILED=()

# =============================================================================
# Helper Functions
# =============================================================================

print_banner() {
    if [[ $NO_COLOR == false ]]; then
        echo -e "${bright_cyan}"
        cat << 'EOF'
╔══════════════════════════════════════════════════════════╗
║                                                          ║
║   macOS Dependency Installer for Marmota              ║
║                                                          ║
╚══════════════════════════════════════════════════════════╝
EOF
        echo -e "${nc}"
    else
        echo "macOS Dependency Installer for Marmota"
        echo "========================================"
    fi
}

print_help() {
    cat << 'EOF'
Usage: ./install-deps-macos.sh [OPTIONS]
   or: ./build.sh --install [OPTIONS]

./build.sh runs this by itself the first time, on a Mac where Marmota
isn't installed yet.

Installation Modes:
  --minimal           Install core dependencies only (Go, git)
  --full              Install core + all optional dependencies (default)
  --dev               Install everything including dev tools

Flags:
  --dry-run           Show what would be installed without installing
  --skip-build        Don't build Marmota, only install dependencies
  --skip-homebrew     Don't check/install Homebrew
  --skip-optional     Skip all optional dependencies
  --skip-exiftool     Skip exiftool installation
  --skip-zoxide       Skip zoxide installation
  --skip-ffmpeg       Skip ffmpeg installation
  --skip-bat          Skip bat installation
  --skip-mupdf        Skip mupdf installation
  --skip-librsvg      Skip librsvg installation
  --skip-fzf          Skip fzf installation
  --skip-wezterm      Skip WezTerm installation
  --skip-nerdfont     Skip Nerd Font installation
  --skip-hotkeys      Skip hotkey configuration (setup-macos-hotkeys.sh)
  --no-color          Disable colored output
  -h, --help          Show this help message

Features:
  • Installs WezTerm (recommended terminal emulator)
  • Installs JetBrains Mono Nerd Font for icon support
  • Configures macOS-style hotkeys via setup-macos-hotkeys.sh
  • Builds Marmota as y, and sets up your shell to run it (cd on quit too)
  • Initializes zoxide for smart directory navigation ('z' command)
  • Moves an install from before y (spf, with y an alias) over to y
  • Supports bash, zsh, and fish shells

Examples:
  ./install-deps-macos.sh                  # Full installation
  ./install-deps-macos.sh --minimal        # Core only
  ./install-deps-macos.sh --dev            # Developer setup
  ./install-deps-macos.sh --full --dry-run # Preview installation
  ./install-deps-macos.sh --skip-ffmpeg    # Skip ffmpeg

EOF
}

log_info() {
    if [[ $NO_COLOR == false ]]; then
        echo -e "${bright_cyan}→${nc} $1"
    else
        echo "→ $1"
    fi
}

log_success() {
    if [[ $NO_COLOR == false ]]; then
        echo -e "${bright_green}✓${nc} $1"
    else
        echo "✓ $1"
    fi
}

log_warning() {
    if [[ $NO_COLOR == false ]]; then
        echo -e "${bright_yellow}⚠${nc} $1"
    else
        echo "⚠ $1"
    fi
}

log_error() {
    if [[ $NO_COLOR == false ]]; then
        echo -e "${bright_red}✗${nc} $1"
    else
        echo "✗ $1"
    fi
}

# =============================================================================
# Argument Parsing
# =============================================================================

parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            --minimal)
                INSTALL_MODE="minimal"
                shift
                ;;
            --full)
                INSTALL_MODE="full"
                shift
                ;;
            --dev)
                INSTALL_MODE="dev"
                shift
                ;;
            --dry-run)
                DRY_RUN=true
                shift
                ;;
            --skip-build)
                SKIP_BUILD=true
                shift
                ;;
            --skip-homebrew)
                SKIP_HOMEBREW=true
                shift
                ;;
            --skip-optional)
                SKIP_OPTIONAL=true
                shift
                ;;
            --skip-exiftool)
                SKIP_EXIFTOOL=true
                shift
                ;;
            --skip-zoxide)
                SKIP_ZOXIDE=true
                shift
                ;;
            --skip-ffmpeg)
                SKIP_FFMPEG=true
                shift
                ;;
            --skip-bat)
                SKIP_BAT=true
                shift
                ;;
            --skip-mupdf)
                SKIP_MUPDF=true
                shift
                ;;
            --skip-librsvg)
                SKIP_LIBRSVG=true
                shift
                ;;
            --skip-fzf)
                SKIP_FZF=true
                shift
                ;;
            --skip-wezterm)
                SKIP_WEZTERM=true
                shift
                ;;
            --skip-nerdfont)
                SKIP_NERDFONT=true
                shift
                ;;
            --skip-hotkeys)
                SKIP_HOTKEYS=true
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
# Validation Functions
# =============================================================================

check_macos() {
    if [[ $(uname -s) != "Darwin" ]]; then
        log_error "This script is for macOS only"
        log_error "Detected OS: $(uname -s)"
        exit 1
    fi
    log_success "Running on macOS ($(sw_vers -productVersion))"
}

# =============================================================================
# Installation Functions
# =============================================================================

install_homebrew() {
    if [[ $SKIP_HOMEBREW == true ]]; then
        log_warning "Skipping Homebrew check (--skip-homebrew)"
        SKIPPED+=("Homebrew")
        return
    fi

    if command -v brew &> /dev/null; then
        log_success "Homebrew already installed ($(brew --version | head -1))"
        return
    fi

    log_info "Homebrew not found, installing..."

    if [[ $DRY_RUN == true ]]; then
        log_info "[DRY RUN] Would install Homebrew"
        return
    fi

    /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"

    if [[ $? -ne 0 ]]; then
        log_error "Failed to install Homebrew"
        exit 1
    fi

    # Handle Apple Silicon PATH
    if [[ $(uname -m) == "arm64" ]]; then
        eval "$(/opt/homebrew/bin/brew shellenv)"
    fi

    log_success "Homebrew installed successfully"
    INSTALLED_CORE+=("Homebrew")
}

install_go() {
    log_info "Checking Go installation..."

    if command -v go &> /dev/null; then
        CURRENT_GO_VERSION=$(go version | grep -oE '[0-9]+\.[0-9]+' | head -1)
        REQUIRED_GO_VERSION="1.25"

        if [[ $(echo -e "$CURRENT_GO_VERSION\n$REQUIRED_GO_VERSION" | sort -V | head -1) == "$REQUIRED_GO_VERSION" ]]; then
            log_success "Go already installed ($(go version | awk '{print $3}'))"
            return
        else
            log_info "Go version $CURRENT_GO_VERSION is outdated (need >= $REQUIRED_GO_VERSION), upgrading..."
            if [[ $DRY_RUN == false ]]; then
                brew upgrade go
                if [[ $? -eq 0 ]]; then
                    log_success "Go upgraded to $(go version | awk '{print $3}')"
                    INSTALLED_CORE+=("Go (upgraded)")
                else
                    log_error "Failed to upgrade Go"
                    FAILED+=("Go upgrade")
                    exit 1
                fi
            else
                log_info "[DRY RUN] Would upgrade Go"
            fi
            return
        fi
    fi

    if [[ $DRY_RUN == true ]]; then
        log_info "[DRY RUN] Would install Go 1.25+"
        return
    fi

    log_info "Installing Go..."
    brew install go

    if [[ $? -eq 0 ]]; then
        log_success "Go installed successfully ($(go version | awk '{print $3}'))"
        INSTALLED_CORE+=("Go")
    else
        log_error "Failed to install Go"
        FAILED+=("Go")
        exit 1
    fi
}

install_git() {
    if command -v git &> /dev/null; then
        log_success "git already installed ($(git --version | awk '{print $3}'))"
        return
    fi

    log_info "Installing git..."

    if [[ $DRY_RUN == true ]]; then
        log_info "[DRY RUN] Would install git"
        return
    fi

    brew install git

    if [[ $? -eq 0 ]]; then
        log_success "git installed successfully"
        INSTALLED_CORE+=("git")
    else
        log_warning "Failed to install git (may already be installed by Xcode)"
        FAILED+=("git")
    fi
}

install_optional_deps() {
    if [[ $INSTALL_MODE == "minimal" ]] || [[ $SKIP_OPTIONAL == true ]]; then
        log_warning "Skipping optional dependencies (mode: $INSTALL_MODE)"
        return
    fi

    echo ""
    log_info "Installing optional dependencies..."

    # exiftool
    if [[ $SKIP_EXIFTOOL == false ]]; then
        if command -v exiftool &> /dev/null; then
            log_success "exiftool already installed"
        else
            if [[ $DRY_RUN == false ]]; then
                log_info "Installing exiftool..."
                brew install exiftool
                if [[ $? -eq 0 ]]; then
                    log_success "exiftool installed"
                    INSTALLED_OPTIONAL+=("exiftool")
                else
                    log_warning "Failed to install exiftool"
                    FAILED+=("exiftool")
                fi
            else
                log_info "[DRY RUN] Would install exiftool"
            fi
        fi
    else
        log_warning "Skipping exiftool (--skip-exiftool)"
        SKIPPED+=("exiftool")
    fi

    # zoxide
    if [[ $SKIP_ZOXIDE == false ]]; then
        if command -v zoxide &> /dev/null; then
            log_success "zoxide already installed"
        else
            if [[ $DRY_RUN == false ]]; then
                log_info "Installing zoxide..."
                brew install zoxide
                if [[ $? -eq 0 ]]; then
                    log_success "zoxide installed"
                    INSTALLED_OPTIONAL+=("zoxide")
                else
                    log_warning "Failed to install zoxide"
                    FAILED+=("zoxide")
                fi
            else
                log_info "[DRY RUN] Would install zoxide"
            fi
        fi
    else
        log_warning "Skipping zoxide (--skip-zoxide)"
        SKIPPED+=("zoxide")
    fi

    # ffmpeg
    if [[ $SKIP_FFMPEG == false ]]; then
        if command -v ffmpeg &> /dev/null; then
            log_success "ffmpeg already installed"
        else
            if [[ $DRY_RUN == false ]]; then
                log_info "Installing ffmpeg (this may take a while)..."
                brew install ffmpeg
                if [[ $? -eq 0 ]]; then
                    log_success "ffmpeg installed"
                    INSTALLED_OPTIONAL+=("ffmpeg")
                else
                    log_warning "Failed to install ffmpeg"
                    FAILED+=("ffmpeg")
                fi
            else
                log_info "[DRY RUN] Would install ffmpeg"
            fi
        fi
    else
        log_warning "Skipping ffmpeg (--skip-ffmpeg)"
        SKIPPED+=("ffmpeg")
    fi

    # bat
    if [[ $SKIP_BAT == false ]]; then
        if command -v bat &> /dev/null || command -v batcat &> /dev/null; then
            log_success "bat already installed"
        else
            if [[ $DRY_RUN == false ]]; then
                log_info "Installing bat..."
                brew install bat
                if [[ $? -eq 0 ]]; then
                    log_success "bat installed"
                    INSTALLED_OPTIONAL+=("bat")
                else
                    log_warning "Failed to install bat"
                    FAILED+=("bat")
                fi
            else
                log_info "[DRY RUN] Would install bat"
            fi
        fi
    else
        log_warning "Skipping bat (--skip-bat)"
        SKIPPED+=("bat")
    fi

    # mupdf (for PDF preview)
    if [[ $SKIP_MUPDF == false ]]; then
        if command -v mutool &> /dev/null; then
            log_success "mupdf already installed"
        else
            if [[ $DRY_RUN == false ]]; then
                log_info "Installing mupdf (for PDF preview)..."
                brew install mupdf
                if [[ $? -eq 0 ]]; then
                    log_success "mupdf installed"
                    INSTALLED_OPTIONAL+=("mupdf")
                else
                    log_warning "Failed to install mupdf"
                    FAILED+=("mupdf")
                fi
            else
                log_info "[DRY RUN] Would install mupdf"
            fi
        fi
    else
        log_warning "Skipping mupdf (--skip-mupdf)"
        SKIPPED+=("mupdf")
    fi

    # librsvg (for SVG preview)
    if [[ $SKIP_LIBRSVG == false ]]; then
        if command -v rsvg-convert &> /dev/null; then
            log_success "librsvg already installed"
        else
            if [[ $DRY_RUN == false ]]; then
                log_info "Installing librsvg (for SVG preview)..."
                brew install librsvg
                if [[ $? -eq 0 ]]; then
                    log_success "librsvg installed"
                    INSTALLED_OPTIONAL+=("librsvg")
                else
                    log_warning "Failed to install librsvg"
                    FAILED+=("librsvg")
                fi
            else
                log_info "[DRY RUN] Would install librsvg"
            fi
        fi
    else
        log_warning "Skipping librsvg (--skip-librsvg)"
        SKIPPED+=("librsvg")
    fi

    # fzf (fuzzy finder for command history, files, etc.)
    if [[ $SKIP_FZF == false ]]; then
        if command -v fzf &> /dev/null; then
            log_success "fzf already installed"
        else
            if [[ $DRY_RUN == false ]]; then
                log_info "Installing fzf (fuzzy finder)..."
                brew install fzf
                if [[ $? -eq 0 ]]; then
                    log_success "fzf installed"
                    INSTALLED_OPTIONAL+=("fzf")
                else
                    log_warning "Failed to install fzf"
                    FAILED+=("fzf")
                fi
            else
                log_info "[DRY RUN] Would install fzf"
            fi
        fi
    else
        log_warning "Skipping fzf (--skip-fzf)"
        SKIPPED+=("fzf")
    fi

    # neovim (default editor for Marmota)
    if command -v nvim &> /dev/null; then
        log_success "neovim already installed"
    else
        if [[ $DRY_RUN == false ]]; then
            log_info "Installing neovim (default editor)..."
            brew install neovim
            if [[ $? -eq 0 ]]; then
                log_success "neovim installed"
                INSTALLED_OPTIONAL+=("neovim")
            else
                log_warning "Failed to install neovim"
                FAILED+=("neovim")
            fi
        else
            log_info "[DRY RUN] Would install neovim"
        fi
    fi
}

install_nvchad() {
    if [[ $INSTALL_MODE == "minimal" ]]; then
        return
    fi

    echo ""
    log_info "Installing NvChad (Neovim configuration)..."

    local nvim_config_dir="$HOME/.config/nvim"

    # Check if NvChad is already installed
    if [[ -d "$nvim_config_dir" ]] && [[ -f "$nvim_config_dir/lua/nvchad/options.lua" ]]; then
        log_success "NvChad already installed"
        return
    fi

    # Check if nvim config exists but is not NvChad
    if [[ -d "$nvim_config_dir" ]]; then
        log_warning "Existing Neovim config found at $nvim_config_dir"
        log_info "Backing up to $nvim_config_dir.backup.$(date +%Y%m%d-%H%M%S)"
        if [[ $DRY_RUN == false ]]; then
            mv "$nvim_config_dir" "$nvim_config_dir.backup.$(date +%Y%m%d-%H%M%S)"
        fi
    fi

    if [[ $DRY_RUN == true ]]; then
        log_info "[DRY RUN] Would install NvChad"
        return
    fi

    log_info "Cloning NvChad starter config..."
    git clone https://github.com/NvChad/starter "$nvim_config_dir"

    if [[ $? -eq 0 ]]; then
        log_success "NvChad installed successfully"
        INSTALLED_OPTIONAL+=("NvChad")
        log_info "Run 'nvim' to complete NvChad setup (plugins will auto-install)"
    else
        log_warning "Failed to install NvChad"
        FAILED+=("NvChad")
    fi
}

install_wezterm() {
    if [[ $INSTALL_MODE == "minimal" ]] || [[ $SKIP_WEZTERM == true ]]; then
        if [[ $SKIP_WEZTERM == true ]]; then
            log_warning "Skipping WezTerm (--skip-wezterm)"
            SKIPPED+=("WezTerm")
        fi
        return
    fi

    echo ""
    log_info "Installing WezTerm (recommended terminal)..."

    # Check if WezTerm is already installed
    if [[ -d "/Applications/WezTerm.app" ]]; then
        log_success "WezTerm already installed"
        return
    fi

    if command -v wezterm &> /dev/null; then
        log_success "WezTerm already installed"
        return
    fi

    if [[ $DRY_RUN == true ]]; then
        log_info "[DRY RUN] Would install WezTerm via Homebrew cask"
        return
    fi

    log_info "Installing WezTerm..."
    brew install --cask wezterm

    if [[ $? -eq 0 ]]; then
        log_success "WezTerm installed successfully"
        INSTALLED_OPTIONAL+=("WezTerm")

        # Check if we created the WezTerm config earlier
        if [[ -f "$HOME/.wezterm.lua" ]]; then
            log_info "WezTerm configuration already exists at ~/.wezterm.lua"
        else
            log_info "To configure WezTerm, create ~/.wezterm.lua"
            log_info "See: https://wezfurlong.org/wezterm/config/files.html"
        fi
    else
        log_warning "Failed to install WezTerm"
        FAILED+=("WezTerm")
    fi
}

install_nerdfont() {
    if [[ $INSTALL_MODE == "minimal" ]] || [[ $SKIP_NERDFONT == true ]]; then
        if [[ $SKIP_NERDFONT == true ]]; then
            log_warning "Skipping Nerd Font (--skip-nerdfont)"
            SKIPPED+=("Nerd Font")
        fi
        return
    fi

    echo ""
    log_info "Installing JetBrains Mono Nerd Font..."

    # Check if font is already installed
    if [[ -f "$HOME/Library/Fonts/JetBrainsMonoNerdFont-Regular.ttf" ]] || \
       [[ -f "/Library/Fonts/JetBrainsMonoNerdFont-Regular.ttf" ]]; then
        log_success "JetBrains Mono Nerd Font already installed"
        return
    fi

    # Check via fc-list if available
    if command -v fc-list &> /dev/null; then
        if fc-list | grep -qi "JetBrainsMono Nerd Font"; then
            log_success "JetBrains Mono Nerd Font already installed"
            return
        fi
    fi

    if [[ $DRY_RUN == true ]]; then
        log_info "[DRY RUN] Would install JetBrains Mono Nerd Font via Homebrew cask"
        return
    fi

    log_info "Installing font (this may take a moment)..."
    brew install --cask font-jetbrains-mono-nerd-font

    if [[ $? -eq 0 ]]; then
        log_success "JetBrains Mono Nerd Font installed successfully"
        INSTALLED_OPTIONAL+=("JetBrains Mono Nerd Font")
        log_info "Font installed to ~/Library/Fonts/"
        log_info "You may need to restart your terminal to use the new font"
    else
        log_warning "Failed to install Nerd Font"
        log_info "You can install manually from: https://www.nerdfonts.com/font-downloads"
        FAILED+=("Nerd Font")
    fi
}

run_hotkey_setup() {
    if [[ $SKIP_HOTKEYS == true ]]; then
        log_warning "Skipping hotkey configuration (--skip-hotkeys)"
        return
    fi

    if [[ $INSTALL_MODE == "minimal" ]]; then
        return
    fi

    echo ""
    log_info "Running hotkey configuration..."

    # Get the directory where this script is located
    local script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    local hotkey_script="$script_dir/setup-macos-hotkeys.sh"

    if [[ ! -f "$hotkey_script" ]]; then
        log_warning "Hotkey setup script not found at $hotkey_script"
        log_info "You can run it manually later: ./setup-macos-hotkeys.sh"
        return
    fi

    if [[ $DRY_RUN == true ]]; then
        log_info "[DRY RUN] Would run: $hotkey_script"
        return
    fi

    # An existing ~/.wezterm.lua is kept: only its Cmd+K changes, so that
    # ./build.sh installing everything never throws away your WezTerm setup
    local hotkey_args=()
    if [[ -f "$HOME/.wezterm.lua" ]]; then
        log_info "Keeping your ~/.wezterm.lua (./setup-macos-hotkeys.sh --wezterm replaces it)"
        hotkey_args=(--neovim --marmota --cmd-k)
    fi

    # Run the hotkey setup script
    bash "$hotkey_script" "${hotkey_args[@]}"

    if [[ $? -eq 0 ]]; then
        log_success "Hotkey configuration completed"
    else
        log_warning "Hotkey configuration encountered issues"
    fi
}

install_dev_tools() {
    if [[ $INSTALL_MODE != "dev" ]]; then
        return
    fi

    echo ""
    log_info "Installing development tools..."

    # golangci-lint
    if command -v golangci-lint &> /dev/null; then
        log_success "golangci-lint already installed"
    else
        if [[ $DRY_RUN == false ]]; then
            log_info "Installing golangci-lint..."
            brew install golangci-lint
            if [[ $? -eq 0 ]]; then
                log_success "golangci-lint installed"
                INSTALLED_DEV+=("golangci-lint")
            else
                log_warning "Failed to install golangci-lint"
                FAILED+=("golangci-lint")
            fi
        else
            log_info "[DRY RUN] Would install golangci-lint"
        fi
    fi
}

build_superfile() {
    if [[ $SKIP_BUILD == true ]]; then
        log_warning "Skipping Marmota build (--skip-build)"
        return
    fi

    echo ""
    log_info "Building Marmota..."

    local script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

    if [[ $DRY_RUN == true ]]; then
        log_info "[DRY RUN] Would build Marmota with: $script_dir/build.sh --no-install"
        return
    fi

    # build.sh builds bin/y, sets up your shell to run it (and moves an
    # install from before y over), and adds new settings to ~/.config/marmota
    if ! Y_BUILD_ONLY=1 "$script_dir/build.sh"; then
        log_error "Failed to build Marmota"
        exit 1
    fi

    log_success "Marmota built successfully"
}

setup_shell_integration() {
    if [[ $SKIP_BUILD == true ]]; then
        return
    fi

    # build.sh, which build_superfile ran, has put the y block in your shell
    # config: ./bin on PATH and the y function (cd on quit). Here: zoxide and
    # fzf in the same config.
    echo ""
    log_info "Setting up zoxide and fzf in your shell..."

    if [[ $DRY_RUN == true ]]; then
        log_info "[DRY RUN] Would add zoxide and fzf to your shell config"
        return
    fi

    local shell_config=""
    case $SHELL in
        */bash) shell_config="$HOME/.bashrc" ;;
        */zsh) shell_config="$HOME/.zshrc" ;;
        */fish) shell_config="$HOME/.config/fish/config.fish" ;;
        *)
            log_warning "Unknown shell: $SHELL: skipping zoxide and fzf setup"
            return
            ;;
    esac
    mkdir -p "$(dirname "$shell_config")"
    touch "$shell_config"

    {
        # Add zoxide initialization if zoxide is installed
        if command -v zoxide &> /dev/null; then
            if ! grep -q "zoxide init" "$shell_config" 2>/dev/null; then
                log_info "Adding zoxide initialization..."

                case $SHELL in
                    */bash)
                        echo '' >> "$shell_config"
                        echo '# Initialize zoxide (smarter cd command)' >> "$shell_config"
                        echo 'eval "$(zoxide init bash)"' >> "$shell_config"
                        ;;
                    */zsh)
                        echo '' >> "$shell_config"
                        echo '# Initialize zoxide (smarter cd command)' >> "$shell_config"
                        echo 'eval "$(zoxide init zsh)"' >> "$shell_config"
                        ;;
                    */fish)
                        echo '' >> "$shell_config"
                        echo '# Initialize zoxide (smarter cd command)' >> "$shell_config"
                        echo 'zoxide init fish | source' >> "$shell_config"
                        ;;
                esac

                if [[ $? -eq 0 ]]; then
                    log_success "Zoxide initialization added (use 'z' command)"
                else
                    log_warning "Failed to add zoxide initialization"
                fi
            else
                log_info "Zoxide already initialized in $shell_config"
            fi
        fi

        # Add fzf keybindings if fzf is installed
        if command -v fzf &> /dev/null; then
            if ! grep -q "fzf.*key-bindings" "$shell_config" 2>/dev/null; then
                log_info "Adding fzf keybindings..."

                # Get fzf installation directory
                local fzf_base=""
                if [[ -d "/opt/homebrew/opt/fzf" ]]; then
                    fzf_base="/opt/homebrew/opt/fzf"
                elif [[ -d "/usr/local/opt/fzf" ]]; then
                    fzf_base="/usr/local/opt/fzf"
                elif [[ -d "$HOME/.fzf" ]]; then
                    fzf_base="$HOME/.fzf"
                fi

                if [[ -n "$fzf_base" ]]; then
                    case $SHELL in
                        */bash)
                            echo '' >> "$shell_config"
                            echo '# fzf fuzzy finder keybindings (Ctrl+R for history, Ctrl+T for files)' >> "$shell_config"
                            echo "source $fzf_base/shell/key-bindings.bash" >> "$shell_config"
                            echo "source $fzf_base/shell/completion.bash" >> "$shell_config"
                            ;;
                        */zsh)
                            echo '' >> "$shell_config"
                            echo '# fzf fuzzy finder keybindings (Ctrl+R for history, Ctrl+T for files)' >> "$shell_config"
                            echo "source $fzf_base/shell/key-bindings.zsh" >> "$shell_config"
                            echo "source $fzf_base/shell/completion.zsh" >> "$shell_config"
                            ;;
                        */fish)
                            echo '' >> "$shell_config"
                            echo '# fzf fuzzy finder keybindings (Ctrl+R for history, Ctrl+T for files)' >> "$shell_config"
                            echo "source $fzf_base/shell/key-bindings.fish" >> "$shell_config"
                            echo "fzf_key_bindings" >> "$shell_config"
                            ;;
                    esac

                    if [[ $? -eq 0 ]]; then
                        log_success "fzf keybindings added (Ctrl+R for history, Ctrl+T for files)"
                    else
                        log_warning "Failed to add fzf keybindings"
                    fi
                else
                    log_warning "fzf installation directory not found"
                fi
            else
                log_info "fzf already configured in $shell_config"
            fi
        fi

        log_info "Run 'source $shell_config' to apply changes in current session"
    }
}

show_nerd_font_instructions() {
    # Only show instructions if font was not installed
    if [[ $SKIP_NERDFONT == true ]] || [[ $INSTALL_MODE == "minimal" ]]; then
        echo ""
        if [[ $NO_COLOR == false ]]; then
            echo -e "${bright_cyan}═══════════════════════════════════════════════════════════${nc}"
            echo -e "${bright_yellow}  Nerd Font Setup (Optional)${nc}"
            echo -e "${bright_cyan}═══════════════════════════════════════════════════════════${nc}"
        else
            echo "═══════════════════════════════════════════════════════════"
            echo "  Nerd Font Setup (Optional)"
            echo "═══════════════════════════════════════════════════════════"
        fi

        cat << 'EOF'

Marmota uses Nerd Font icons for the best visual experience.
You skipped the automatic installation, but you can still install manually:

To install a Nerd Font:
1. Visit: https://www.nerdfonts.com/font-downloads
2. Download your preferred font (e.g., "JetBrains Mono Nerd Font")
3. Install the font files (double-click .ttf files)
4. Configure your terminal to use the Nerd Font

Or run this command:
  brew install --cask font-jetbrains-mono-nerd-font

If you skip this, set 'nerdfont = false' in:
  ~/.config/marmota/config.toml

EOF
    fi
}

print_summary() {
    echo ""
    if [[ $NO_COLOR == false ]]; then
        echo -e "${bright_cyan}═══════════════════════════════════════════════════════════${nc}"
        echo -e "${bright_green}  Installation Summary${nc}"
        echo -e "${bright_cyan}═══════════════════════════════════════════════════════════${nc}"
    else
        echo "═══════════════════════════════════════════════════════════"
        echo "  Installation Summary"
        echo "═══════════════════════════════════════════════════════════"
    fi

    echo ""
    echo "Core Dependencies:"
    if command -v go &> /dev/null; then
        log_success "Go: $(go version | awk '{print $3}')"
    else
        log_warning "Go: Not installed"
    fi

    if command -v git &> /dev/null; then
        log_success "git: $(git --version | awk '{print $3}')"
    else
        log_warning "git: Not installed"
    fi

    if command -v brew &> /dev/null; then
        log_success "Homebrew: $(brew --version | head -1 | awk '{print $2}')"
    else
        log_warning "Homebrew: Not installed"
    fi

    echo ""
    echo "Terminal & Editor:"
    if [[ -d "/Applications/WezTerm.app" ]] || command -v wezterm &> /dev/null; then
        log_success "WezTerm: Installed (recommended terminal)"
    else
        log_warning "WezTerm: Not installed (consider installing for best experience)"
    fi

    # Check for Nerd Font
    if [[ -f "$HOME/Library/Fonts/JetBrainsMonoNerdFont-Regular.ttf" ]] || \
       [[ -f "/Library/Fonts/JetBrainsMonoNerdFont-Regular.ttf" ]]; then
        log_success "JetBrains Mono Nerd Font: Installed"
    else
        if command -v fc-list &> /dev/null && fc-list | grep -qi "JetBrainsMono Nerd Font"; then
            log_success "JetBrains Mono Nerd Font: Installed"
        else
            log_warning "Nerd Font: Not installed (icons may not display correctly)"
        fi
    fi

    # Check for WezTerm config
    if [[ -f "$HOME/.wezterm.lua" ]]; then
        log_success "WezTerm Config: Created at ~/.wezterm.lua"
    fi

    # Check for NvChad
    if [[ -d "$HOME/.config/nvim" ]] && [[ -f "$HOME/.config/nvim/lua/nvchad/options.lua" ]]; then
        log_success "NvChad: Installed at ~/.config/nvim"
    else
        log_warning "NvChad: Not installed"
    fi

    if [[ ${#INSTALLED_OPTIONAL[@]} -gt 0 ]] || [[ $INSTALL_MODE != "minimal" ]]; then
        echo ""
        echo "Optional Plugins:"

        if command -v exiftool &> /dev/null; then
            log_success "exiftool: Installed (metadata plugin)"
        else
            log_warning "exiftool: Not installed"
        fi

        if command -v zoxide &> /dev/null; then
            log_success "zoxide: Installed (directory navigation)"
        else
            log_warning "zoxide: Not installed"
        fi

        if command -v ffmpeg &> /dev/null; then
            log_success "ffmpeg: Installed (video thumbnails)"
        else
            log_warning "ffmpeg: Not installed"
        fi

        if command -v bat &> /dev/null || command -v batcat &> /dev/null; then
            log_success "bat: Installed (code highlighting)"
        else
            log_warning "bat: Not installed"
        fi

        if command -v mutool &> /dev/null; then
            log_success "mupdf: Installed (PDF preview)"
        else
            log_warning "mupdf: Not installed"
        fi

        if command -v rsvg-convert &> /dev/null; then
            log_success "librsvg: Installed (SVG preview)"
        else
            log_warning "librsvg: Not installed"
        fi

        if command -v fzf &> /dev/null; then
            log_success "fzf: Installed (fuzzy finder, Ctrl+R for history)"
        else
            log_warning "fzf: Not installed"
        fi

        if command -v nvim &> /dev/null; then
            log_success "neovim: Installed (default editor)"
        else
            log_warning "neovim: Not installed"
        fi
    fi

    if [[ $INSTALL_MODE == "dev" ]]; then
        echo ""
        echo "Development Tools:"
        if command -v golangci-lint &> /dev/null; then
            log_success "golangci-lint: Installed"
        else
            log_warning "golangci-lint: Not installed"
        fi
    fi

    if [[ $SKIP_BUILD == false ]]; then
        echo ""
        echo "Marmota Binary:"
        local script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
        if [[ -f "$script_dir/bin/y" ]]; then
            log_success "Built: $script_dir/bin/y (the y shell function runs it)"
        else
            log_error "Not built"
        fi
    fi

    if [[ ${#FAILED[@]} -gt 0 ]]; then
        echo ""
        echo "Failed installations:"
        for item in "${FAILED[@]}"; do
            log_error "$item"
        done
    fi

    echo ""
    if [[ $NO_COLOR == false ]]; then
        echo -e "${bright_cyan}═══════════════════════════════════════════════════════════${nc}"
        echo -e "${bright_green}  Next Steps${nc}"
        echo -e "${bright_cyan}═══════════════════════════════════════════════════════════${nc}"
    else
        echo "═══════════════════════════════════════════════════════════"
        echo "  Next Steps"
        echo "═══════════════════════════════════════════════════════════"
    fi

    cat << 'EOF'

1. Launch WezTerm (recommended terminal):
   • Find it in Applications or Spotlight
   • WezTerm has macOS-style hotkeys configured:
     - Cmd+Arrow: Jump to line start/end
     - Opt+Arrow: Jump by word
     - Opt+Backspace: Delete word
     - Cmd+T/D: New tab / Split horizontal

2. Restart terminal or run 'source ~/.zshrc' to enable shell integration

3. Run 'y' to start Marmota
   • Press 'Q' to quit and cd to the current directory
   • Press 'q' to quit normally
   • Press 'e' or 'E' to open file in neovim

4. Use 'z' command for smart directory navigation (zoxide):
   • 'z <directory>' - Jump to a frequently used directory
   • 'z foo' - Jump to /path/to/foo (learns from your cd history)

5. Use fzf fuzzy finder:
   • Ctrl+R - Search command history
   • Ctrl+T - Search and insert file paths

6. After pulling new code, rebuild with ./build.sh
   • ./build.sh --install runs this whole installation again

7. Reconfigure hotkeys anytime: ./setup-macos-hotkeys.sh
   • ./setup-macos-hotkeys.sh --wezterm replaces an existing ~/.wezterm.lua

8. A tour of Marmota: docs/tutorial.html; ? in Marmota lists every key

Everything is ready! Launch WezTerm and run 'y'.

EOF
}

# =============================================================================
# Main Execution
# =============================================================================

main() {
    parse_args "$@"

    print_banner

    if [[ $DRY_RUN == true ]]; then
        log_warning "DRY RUN MODE - No changes will be made"
        echo ""
    fi

    log_info "Installation mode: $INSTALL_MODE"
    echo ""

    # Platform verification
    check_macos
    echo ""

    # Core dependencies
    log_info "Installing core dependencies..."
    install_homebrew
    install_go
    install_git

    # Optional dependencies
    if [[ $INSTALL_MODE == "full" ]] || [[ $INSTALL_MODE == "dev" ]]; then
        install_optional_deps
    fi

    # NvChad (Neovim configuration)
    install_nvchad

    # WezTerm (recommended terminal)
    install_wezterm

    # Nerd Font
    install_nerdfont

    # Development tools
    if [[ $INSTALL_MODE == "dev" ]]; then
        install_dev_tools
    fi

    # Build Marmota
    build_superfile

    # Setup shell integration for cd-on-quit
    setup_shell_integration

    # Run hotkey configuration (WezTerm, Neovim, Marmota)
    run_hotkey_setup

    # Show manual Nerd Font instructions if skipped
    show_nerd_font_instructions

    # Summary
    print_summary

    if [[ $DRY_RUN == false ]] && [[ ${#FAILED[@]} -eq 0 ]]; then
        log_success "Installation complete!"
    elif [[ $DRY_RUN == true ]]; then
        log_info "Dry run complete. Run without --dry-run to install."
    else
        log_warning "Installation completed with some failures (see summary above)"
        exit 1
    fi
}

# Run main function
main "$@"
