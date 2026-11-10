# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Shell Scripts

| Script | Purpose |
|--------|---------|
| `build.sh` | The one command to install and build. On a Mac with no Go, or nothing in a shell config that runs Marmota, it hands over to `install-deps-macos.sh` (which calls back with `Y_BUILD_ONLY=1`); `--install [args]` forces that, `--no-install` skips it. Otherwise: builds `./bin/y` (the binary is `y`), deletes old `spf` binaries that say they are Marmota, writes the `# >>> y (Marmota) >>>` block in the shell configs that ran Marmota (PATH + the `y` function for cd on quit; an old `Superfile cd-on-quit wrapper` block and `alias y='spf'` are taken out, the file backed up), runs `--fix-config-file`. |
| `dev.sh` | Full dev workflow - runs `go mod tidy`, `go fmt`, `golangci-lint`, tests, then calls `build.sh`. Use `--skip-tests` to skip tests, `--testsuite` for integration tests. |
| `install-deps-macos.sh` | macOS setup, run by `build.sh` the first time - installs Homebrew, Go, optional deps (exiftool, zoxide, ffmpeg, bat, mupdf, fzf), NvChad, WezTerm, Nerd Font, builds through `build.sh`, configures shell integration for bash/zsh/fish, then runs `setup-macos-hotkeys.sh` (only `--neovim --marmota --cmd-k` when `~/.wezterm.lua` exists). |
| `setup-macos-hotkeys.sh` | Configures WezTerm hotkeys and Marmota editor settings (nvim for `e`, nvim in new tab for `E`), and installs `gy` for nvim (`scripts/nvim/spf_yank_ref.lua`: yanks `@file#L12-15` references). `--neovim` does only the last. `--cmd-k` only makes Cmd+K (WezTerm's Clear) leave Marmota alone in an existing `~/.wezterm.lua`. |

## Build & Development Commands

```bash
# Quick build (detects user's binary location)
./build.sh

# Development workflow (format + lint + tests + build)
make dev          # or ./dev.sh

# Build only (skip tests)
make build        # or ./dev.sh --skip-tests

# Run tests
make test         # or go test ./...

# Run linter
make lint         # or golangci-lint run

# Run full testsuite
make testsuite    # or ./dev.sh --testsuite

# Format code (required before PR)
go fmt ./...

# First-time macOS setup (installs all dependencies)
./install-deps-macos.sh

# Configure hotkeys only
./setup-macos-hotkeys.sh
```

## Architecture Overview

Marmota is a terminal file manager built with [Bubbletea](https://github.com/charmbracelet/bubbletea) (Elm-inspired TUI framework).

### Entry Points
- `main.go` - Embeds config files, delegates to `cmd.Run()`
- `src/cmd/main.go` - CLI setup via `urfave/cli`, creates `tea.Program` with `internal.InitialModel()`

### Core Package Structure

| Package | Purpose |
|---------|---------|
| `src/internal` | Main model, file operations, key handling, search logic |
| `src/internal/common` | Shared config state, styling, hotkey management |
| `src/internal/ui` | UI components (jump: the s popup; metadata, processbar, prompt; sidebar and zoxide: the data the s popup lists) |
| `src/internal/ui/rendering` | Renderer system for file panels with borders/truncation |
| `src/config` | Fixed variables (paths, versions), type definitions |
| `src/pkg/file_preview` | File preview (syntax highlighting, images, PDFs) |
| `src/pkg/claudesession` | Finds Claude Code sessions: live ones from `claude agents --json`, details and history from `~/.claude/projects` transcripts |
| `src/pkg/claudeask` | The Ask box's requests: the context block, and the permission mode, model and effort flags |
| `src/pkg/codexsession` | Codex CLI sessions, in claudesession's form: the Codex server's list, state, sending, renaming and interrupting (JSON-RPC over a WebSocket on `~/.codex/app-server-control/app-server-control.sock`), and each session's rollout (conversation, changes) |

### Bubbletea Pattern

The app follows Model-Update-View:
- `src/internal/type.go` - Main `model` struct with all state
- `src/internal/model.go` - `Update()` and `View()` methods
- `src/internal/model_msg.go` - Message types for async operations

Key message types: `PasteOperationMsg`, `DeleteOperationMsg`, `FilePreviewUpdateMsg`, `MetadataMsg`, `GitStatusUpdateMsg`

### File Operations
- `src/internal/file_operations.go` - Copy, move, delete, trash operations
- `src/internal/handle_file_operations.go` - Handlers for paste/delete/compress/extract
- Async operations run in goroutines with progress tracked via `processbarModel`

### Search System
- `src/internal/search_stack.go` - Hierarchical search with layers (regular, recursive, filter)
- Supports file type filters: `f:` for files, `d:` for directories

### Configuration
- User configs: `~/.config/marmota/` (config.toml, hotkeys.toml, theme files)
- Embedded defaults: `src/superfile_config/`
- State files: `~/.local/state/Marmota/`

## Key Files for Common Tasks

| Task | Files |
|------|-------|
| Add new hotkey | `src/internal/common/config_type.go`, `src/superfile_config/hotkeys.toml` and `vimHotkeys.toml`, `src/internal/key_function.go`, plus a help entry in `src/internal/default_config.go` (`TestHelpMenuListsEveryHotkey` fails without one) |
| Modify file operations | `src/internal/file_operations.go`, `src/internal/handle_file_operations.go` |
| The `s` popup: go to a pinned folder or file, a session (running, or ended to resume), a folder zoxide knows, a file edited lately. Two columns (Pinned, Sessions, Resume · Folders, Files), one when narrow. Searching: enter goes to the first match (↵), a pin, folder or file before a session, home with nothing typed. Picking (↓): numbered rows, `[n]g`/`G`, and each item takes the keys it takes elsewhere (t pin, r rename on disk or a pin's name, a session's are the Sessions box's). esc steps back: picking → search → clear → close. `z` is unbound | `src/internal/ui/jump/` (modes, layout, matching whole words), `src/internal/jump.go` (the sections, enter and each item's keys), `src/internal/ui/sidebar/` (pins and recent files: `entries.go`), `src/internal/ui/zoxide/` (`Query`) |
| Change file panel rendering | `src/internal/ui/rendering/`, `src/internal/render.go` |
| Add new modal | `src/internal/ui/`, then integrate in `model.go` |
| Claude sessions (status line, `!` `@` `=`) | `src/internal/claude_sessions.go`, `src/pkg/claudesession/` |
| Codex sessions (listed with Claude Code's; `Session.Agent` tells them apart; never start one with `-c`/`--config` or `--approve-for-me`: Codex then runs it on a server of its own, out of superfile's sight) | `src/internal/codex_sessions.go` (their panes, by the title Codex sets, a fork's by the pane it was started in; resume, fork, send, a start's session found by folder and time), `src/pkg/codexsession/` |
| Typing into a session's prompt (rename, `/cd`, the Ask box to a running session) | `src/internal/claude_prompt.go`, `src/pkg/claudeprompt/` |
| Inbox (toasts, OSC 9 notifications, `!`) and Presence (✳ and git letters in file rows) | `src/internal/claude_inbox.go`, `src/internal/presence.go`, `src/internal/ui/sidebar/git_status.go` |
| Claude sessions shared by the dock (no sessions mode: it was removed for the dock) | `src/internal/sessions_state.go` (the list's sessions, the selected one's conversation, scrolling it), `src/internal/sessions_pane.go` (the session the preview draws), `src/internal/sessions_actions.go` (rename, fork, stop), `src/internal/claude_sessions_order.go` (the day's order), `src/internal/ui/sessions/` (the list: selection and rename) |
| A session's files (`W`) and their diff in the preview (`]` `[`) | `src/internal/session_files.go` (a search layer over the panel's folder), `src/pkg/claudesession/filehistory.go` (Claude Code's backups, `@v1` the original; shell changes from `bashEditDiff`) |
| Ask box at the foot of file mode (`K` opens it over Details, `esc` folds it; `m` mentions in its draft) | `src/internal/dock_ask.go` (its draft is `m.sessions.composer`; who it writes to: `dockAskTarget`) |
| `V`: the preview pinned to a session while you browse files | `src/internal/viewer_pin.go` (`m.dock.pinned`; the preview shows the session the Sessions box is on) |
| Sessions box at the foot of file mode (`!` in and out, the preview showing the selected session; each row says cc for Claude Code, co for Codex) | `src/internal/dock_sessions.go` (moves `m.sessions.list`) |
| Details box at the foot (FILE, MAC, OPS; `M` focuses it, `F` hides it) | `src/internal/details.go` (what it shows, the machine readings every 2 s, the status line meter while it is hidden), `src/internal/ui/details/` (the box), `src/pkg/sysstat/` (cpu, memory, disk via gopsutil) |
| Mouse wheel over the previews and Details (nothing else takes the mouse) | `src/internal/mouse.go` (what is under the pointer), `src/internal/ui/preview/scroll.go` (the file preview's document, drawn on the first scroll) |
| The shelf (`Y`: the selection, or in the Sessions box the files the session changed, as one pile on a panel at the screen's right edge to drag into another app; piles stay after a drop, and the shelf until you close it; `shelf_app` names an app such as Yoink instead) | `src/internal/shelf.go`, `scripts/shelf/main.swift` (built into `bin/y-shelf` by `build.sh`; a second `Y` adds to the open shelf through a distributed notification) |
| Resuming an ended session from the Ask box (`ctrl+r` while it writes to a new session opens the `s` popup on its Resume section alone: the five Claude Code and five Codex sessions used last; with a draft the picked one is resumed in the background with the draft as its first message, alone beside Marmota) | `src/internal/ask_resume.go` (reading older ended sessions through `claudesession.Ended`, the resume commands), `src/internal/jump.go` (`openJumpForResume`) |
| Answering a session's question from the Sessions box | `src/internal/claude_answer.go` (reads it off the pane, sends keys) |
| Agent panes beside Marmota | `src/internal/wezterm_layout.go`, `src/internal/managed_agents.go` (hidden panes wait in the `superfile-agents` WezTerm workspace) |
| Ask box sending, and the session in the preview | `src/internal/ui/ask/` (the composer: `tab` starts new sessions in Claude Code or Codex, from Claude Code · default model · medium after each start; `ctrl+o`/`ctrl+t` also change a running Codex session's model and effort from its next message, not Claude Code's, whose /model and /effort would save them as defaults; `pane.go` the session the preview draws; `markdown.go` and `table.go` replies), `src/internal/ask_bar.go` (who it sends to, the draft, starting sessions in the background and holding what you send them until their first reply), `src/internal/starting_sessions.go` (a started session's row until it is listed, and the question it asks first, cut out of its pane), `src/pkg/claudeask/` (context block, flags) |

## Testing Claude session features

- Unit and model tests never run the real `claude` CLI: `TestMain` in `src/internal/model_test.go` swaps in a fake runner, and makes the WezTerm and clipboard seams fail unless a test fakes them (`newFakeWezterm`, `newFakeClaudePane`).
- `python3 testsuite/live/wezterm_sessions_test.py --binary ./bin/y` drives a real Marmota inside WezTerm, in a workspace your window doesn't show, with a fake `claude` from `testsuite/live/fakes/`. Its sessions draw a prompt box like Claude Code's (`fake_prompt.py`), since Marmota reads it before typing into a session.
- The tutorial's screenshots (`docs/shots`) are real sessions on demo projects; `testsuite/shots/README.md` says how to retake them without your own sessions or machine showing.

## Platform-Specific Notes

- macOS: Uses Finder via osascript for trashing protected files
- Windows: Not fully supported yet
- Trash: XDG trash on Linux, ~/.Trash on macOS, trash-go on Windows

## Running it

The binary is `y` (it was `spf`). `y` in a shell is the function `build.sh` writes, which runs `<repository>/bin/y`; there is no alias.
