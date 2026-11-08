# macOS Dependency Installation Guide

This guide helps you install all dependencies needed to build and run Marmota with full functionality on macOS.

## Quick Start

`./build.sh` runs this script by itself the first time (no Go, or nothing in your shell config runs Marmota).
`./build.sh --install [OPTIONS]` runs it again with any of the options below.

```bash
# Full installation (recommended for most users)
./install-deps-macos.sh

# Preview what would be installed (dry-run)
./install-deps-macos.sh --dry-run

# Minimal installation (core only)
./install-deps-macos.sh --minimal

# Developer setup (includes linting tools)
./install-deps-macos.sh --dev
```

## Dependencies Overview

### Core Dependencies (Required)
- **Go 1.25.0+** - Required for building Marmota
- **git** - For repository operations (usually pre-installed)
- **Homebrew** - Package manager for macOS

### Optional Runtime Dependencies (Plugins)
- **exiftool** - Enhanced file metadata display
  - Enable in config: `metadata = true`
  - Provides detailed EXIF data from images and files

- **zoxide** - Smart directory navigation
  - Enable in config: `zoxide_support = true`
  - Press `z` key to open quick directory jumper

- **ffmpeg** - Video thumbnail generation
  - Automatically detected at runtime
  - Generates previews for video files

- **bat** - Alternative code syntax highlighting
  - Enable in config: `code_previewer = "bat"`
  - Fallback: Built-in chroma library (default)

- **mupdf** - PDF file preview support
  - Automatically detected at runtime
  - Provides `mutool` command for rendering PDF pages
  - Generates previews for PDF files

### Development Dependencies (Optional)
- **golangci-lint** - Code linting for contributors

### Manual Setup (Recommended)
- **Nerd Font** - Required for proper icon display
  - Download from: https://www.nerdfonts.com/font-downloads
  - Recommended fonts: "Hack Nerd Font", "JetBrains Mono Nerd Font"
  - Configure in your terminal settings
  - Alternative: Set `nerdfont = false` in config

## Installation Modes

### Minimal Mode
Installs only core dependencies (Go, git).

```bash
./install-deps-macos.sh --minimal
```

**Use when:** You just want to try Marmota without optional features.

### Full Mode (Default)
Installs core + all optional dependencies.

```bash
./install-deps-macos.sh --full
# or simply
./install-deps-macos.sh
```

**Use when:** You want the complete Marmota experience.

### Dev Mode
Installs everything including development tools.

```bash
./install-deps-macos.sh --dev
```

**Use when:** You're contributing to Marmota development.

## Script Options

### Installation Control
- `--minimal` - Core dependencies only
- `--full` - Core + optional dependencies (default)
- `--dev` - Everything including dev tools
- `--dry-run` - Preview without installing
- `--skip-build` - Install dependencies only, don't build

### Skip Specific Dependencies
- `--skip-homebrew` - Don't check/install Homebrew
- `--skip-optional` - Skip all optional dependencies
- `--skip-exiftool` - Skip exiftool
- `--skip-zoxide` - Skip zoxide
- `--skip-ffmpeg` - Skip ffmpeg
- `--skip-bat` - Skip bat
- `--skip-mupdf` - Skip mupdf

### Display Options
- `--no-color` - Disable colored output
- `-h, --help` - Show help message

## Example Usage

### Check what would be installed
```bash
./install-deps-macos.sh --full --dry-run
```

### Install core + specific plugins
```bash
./install-deps-macos.sh --full --skip-ffmpeg --skip-bat
```

### Install dependencies only (no build)
```bash
./install-deps-macos.sh --full --skip-build
```

### Developer setup with preview
```bash
./install-deps-macos.sh --dev --dry-run
```

## After Installation

### 1. Enable Plugins

Edit `~/.config/marmota/config.toml`:

```toml
# Enhanced metadata display (requires exiftool)
metadata = true

# Smart directory navigation (requires zoxide)
zoxide_support = true

# Alternative syntax highlighting (requires bat)
code_previewer = "bat"
```

### 2. Configure Nerd Font

For best visual experience:
1. Install a Nerd Font from https://www.nerdfonts.com/
2. Configure your terminal to use it:
   - **iTerm2**: Preferences → Profiles → Text → Font
   - **Terminal.app**: Preferences → Profiles → Font
   - **Warp**: Settings → Appearance → Terminal Font
   - **Alacritty**: Edit `~/.config/alacritty/alacritty.yml`
   - **Ghostty**: Settings → Font

If you skip this, set `nerdfont = false` in config.toml.

### 3. Run Marmota

In a new terminal:

```bash
y
```

## Troubleshooting

### `y` command not found after installation
- `build.sh` puts a block marked `# >>> y (Marmota) >>>` in your `~/.zshrc` (or `~/.bashrc`, fish's `config.fish`): it adds the repository's `bin` to your PATH and defines the `y` function, which runs `bin/y`
- Reload your shell: `source ~/.zshrc` (or your shell's config)
- `y` still starts something else (yazi, say): a `y` defined after that block wins; the block is meant to come last

### Go version too old
- The script will automatically upgrade Go via Homebrew
- Manual upgrade: `brew upgrade go`

### Homebrew installation fails
- Follow Homebrew's official installation guide
- Visit: https://brew.sh/

### Optional dependencies fail to install
- These are non-critical, Marmota will work without them
- Try installing manually: `brew install <package>`
- Use `--skip-<package>` to skip specific dependencies

## Manual Installation

If you prefer to install dependencies manually:

```bash
# Core dependencies
brew install go git

# Optional dependencies
brew install exiftool zoxide ffmpeg bat mupdf

# Development tools (optional)
brew install golangci-lint

# Build Marmota (it runs as y)
CGO_ENABLED=0 go build -o ./bin/y
sudo mv ./bin/y /usr/local/bin/y
```

## Documentation

- [README.md](./README.md): what Marmota does, its keys and its settings
- [Getting started with Marmota](https://junjieliang672.github.io/marmota/tutorial.html): a tour for your first session
- `?` in Marmota lists every key

## Support

If you encounter issues:
1. Check the troubleshooting section above
2. Run with `--dry-run` to see what would be installed
3. Try `--minimal` mode first, then add optional dependencies
4. Report issues at: https://github.com/junjieliang672/marmota/issues
