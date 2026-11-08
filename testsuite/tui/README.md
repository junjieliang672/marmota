# tui: drive the real spf in a pseudo-terminal

`tui` runs the real `spf` binary in a pty (no tmux), emulates the screen with
[pyte](https://github.com/selectel/pyte), answers terminal queries, and can
render the screen to a PNG. Every session runs in a throwaway HOME, so tests
never read or write your real Marmota config, state, zoxide database,
clipboard, Trash, WezTerm or `~/.claude`.

## Setup

From `testsuite/` (Python 3.9+; uses the same `.venv` as the tmux suite):

```
python3 -m venv .venv
.venv/bin/pip install -r requirements.txt -r tui/requirements.txt
```

## Running

```
cd testsuite
.venv/bin/python -m tui --spf-path ../bin/y          # all tests in tui/tests
.venv/bin/python -m tui --spf-path ../bin/y -t QuitTest --shots -d
```

* `--spf-path` (or `$SPF_BINARY`, default `../bin/y`): binary under test.
* `--shots`: also save a final screenshot of every session when tests pass.
* `--config-source binary`: seed the config the binary itself embeds instead
  of `src/superfile_config` (use this when testing an older build while the
  working tree has moved on).
* `-x` stop at the first failure, `--keep-sandboxes` keep the temp HOMEs.

On failure the runner writes `tui/artifacts/<Test>/failure.png`, `.txt`
(screen text, modes, answered queries, traceback) and `.raw` (pty output),
plus y's `marmota.log` and the fake tools' logs. `tui/artifacts/` is
gitignored.

Quick screenshot:

```
.venv/bin/python -m tui.shot --binary ../bin/y --size 150x40 --cwd DIR \
    --keys "J J ctrl+k" --out shot.png      # extra tokens: wait:TEXT sleep:0.5 stable text:LITERAL
```

The tmux suite has no isolation of its own; run it inside a sandbox with:

```
.venv/bin/python -m tui.run_in_sandbox -- .venv/bin/python main.py --spf-path ../bin/y
```

## Writing tests

Put a `*_test.py` file in `tui/tests/`; every `TuiTest` subclass whose name
ends with `Test` runs (same rule as `core/runner.py`). Raise to fail.

```python
from tui.runner import TuiTest

class RenameTest(TuiTest):
    def run(self) -> None:
        sandbox = self.ctx.sandbox()                      # fresh HOME, seeded config
        work = sandbox.workdir(files={"a.txt": "hi"})
        spf = self.ctx.session(cols=120, rows=40, cwd=work, sandbox=sandbox)
        spf.wait_for("a.txt")
        spf.send("r", "ctrl+u")                           # one key per write
        spf.type_text("b").send("enter")                  # rename keeps ".txt"
        spf.wait_for("b.txt")
        assert (work / "b.txt").exists()
        self.ctx.snap("renamed")                          # tui/artifacts/RenameTest/renamed.png
```

`Session(binary, cols, rows, cwd, env)` can also be used directly as a
context manager. Main API:

* `send(*keys)`: key specs such as `enter esc tab shift+tab space backspace
  up down left right home end pgup pgdown delete f1..f12 ctrl+x alt+x
  alt+shift+right`, single characters, or literal text; `type_text(text)`,
  `send_raw(bytes)`, `paste(text)`, `click(row, col)`, `focus(bool)`.
* `wait_for(text_or_regex)` returns `(row, col)`; `wait_gone`, `wait_stable`,
  `wait_exit` (returns the exit code). Timeouts raise `WaitTimeout` with the
  screen in the message.
* `screen_text()`, `text()`, `dump()`, `find()`, `find_all()`, `contains()`;
  `cell(row, col)`, `row(row)`, `cells_of(text)` give `Cell`s with resolved
  `fg`/`bg` (`#rrggbb`), raw pyte colors, bold/italic/underline/reverse/wide.
* `cursor`, `cursor_visible`, `title`, `modes` (e.g. 1049 alt screen),
  `graphics` (Kitty/sixel images sent), `osc52_clipboard`, `raw_output()`.
* `screenshot(path)` renders a PNG (Menlo/SF Mono, Nerd Font for icons,
  CJK/emoji fallbacks, box drawing and half blocks drawn as shapes).
* `wait_until(predicate)` polls for side effects that are not on screen.

## Isolation

`Sandbox` builds the environment from scratch: `HOME` and
`XDG_{CONFIG,DATA,STATE,CACHE}_HOME` point into a temp dir (spf uses
`$HOME/.config/marmota` on macOS and the XDG dirs for data, state and
cache), plus `TMPDIR`, `_ZO_DATA_DIR`, `CLAUDE_CONFIG_DIR`. Nothing else is
inherited from your shell except `PATH`, `USER` and `LOGNAME`, so
`WEZTERM_PANE`, `TERM_PROGRAM` and `CLAUDE*` are unset unless a test passes
them in `env`. `HOME`/`XDG_*` overrides that point outside the sandbox are
refused. Each sandbox gets `config.toml`, `hotkeys.toml` and `theme/` from
`src/superfile_config`, `firstUseCheck` (no intro modal;
`Sandbox(first_use=True)` for it) and `~/.Trash`.

`tui/fakes` comes first on `PATH`. Each fake appends its argv as a JSON array
per line to a log (`sandbox.fake_calls("wezterm")`):

| fake | behavior |
|------|----------|
| `claude` | `--version` prints `2.1.318 (Claude Code)`; `agents --json [--all] [--cwd P]` prints `$FAKE_CLAUDE_AGENTS_JSON` (`sandbox.set_claude_agents([...])`, default `[]`, logged to `claude-queries`); anything else is logged to `$FAKE_CLAUDE_LOG` and exits 0 |
| `wezterm` | logs every call; `cli list` prints `$FAKE_WEZTERM_LIST` (`sandbox.set_wezterm_panes`), `cli split-pane`/`cli spawn` print pane ids from 100 up, `cli get-text` prints `$FAKE_WEZTERM_TEXT`; `$FAKE_WEZTERM_FAIL=kill-pane,...` makes subcommands fail |
| `pbcopy`/`pbpaste` | sandbox clipboard file (`sandbox.clipboard()`) |
| `open`/`xdg-open` | log only, no GUI apps |
| `osascript`, `sudo` | log and fail, so Finder or a password prompt is never reached |

Fixture variables hold a file path (re-read on every call, so a test can
change them while spf runs) or literal content. For transcripts use
`Sandbox(claude_home=True)` and
`sandbox.add_claude_transcript(project_dir, session_id, records)`, which
writes `~/.claude/projects/<slug>/<id>.jsonl` inside the sandbox. To use
WezTerm code paths, pass `env={"WEZTERM_PANE": "1"}` and a pane list.

## Limitations

* Kitty/iTerm/sixel images are not drawn (pyte has no graphics). spf only
  uses them when `TERM_PROGRAM`/`KITTY_WINDOW_ID` say so; by default it falls
  back to half blocks, which render correctly. With `TERM_PROGRAM=WezTerm`
  the image commands are recorded in `session.graphics` instead.
* pyte has no alternate screen: after spf exits, the last frame stays on
  screen, which is handy for failure screenshots.
* Only legacy key encoding is sent; `ctrl+shift+<letter>` cannot be expressed
  and raises `KeySpecError`.
* Keys are paced (`key_delay`, 50 ms; 100 ms after `esc`) because Bubble Tea
  merges input that arrives in one read (`JJ` instead of `J`, `esc q` becomes
  `alt+q`). Prefer `wait_for` over sleeps; `wait_stable` ignores attribute
  changes (blinking cursors) but not spinners.
* Width tables: pyte uses `wcwidth`, spf uses go-runewidth (we set
  `RUNEWIDTH_EASTASIAN=0`); they agree for box drawing, Nerd Font icons and
  CJK, but an exotic emoji sequence could still disagree.
* `e`/`E` start the configured editors (`nvim`, `neovide`) for real; override
  `editor`/`second_editor` through `Sandbox(config_overrides=...)`.
