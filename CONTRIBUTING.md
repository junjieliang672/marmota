# Contributing

## Bugs and ideas

Open an issue at https://github.com/junjieliang672/marmota/issues. For a bug, say what you
did, what you expected and what happened, with `y --version`, your macOS version and your terminal
(WezTerm and its version).

## Changing the code

```bash
make dev       # go fmt, golangci-lint, the tests, then ./build.sh
go test ./...  # the tests only
python3 testsuite/live/wezterm_sessions_test.py --binary ./bin/y   # a real y in WezTerm, with a fake claude
```

[CLAUDE.md](./CLAUDE.md) maps the code: where the file panels, the Sessions and Ask boxes, Claude Code
and Codex sessions, the previews and the WezTerm panes live.

- **A new hotkey**: `src/internal/common/config_type.go`, `src/superfile_config/hotkeys.toml` and
  `vimHotkeys.toml`, `src/internal/key_function.go`, and a help entry in
  `src/internal/default_config.go` (`TestHelpMenuListsEveryHotkey` fails without one).
- **A new theme**: copy a file in `src/superfile_config/theme/`, rename it, change its colors.

## Pull requests

- `make dev` passes: formatted, linted, tested.
- The description says what changes for someone using y, and how you checked it.
