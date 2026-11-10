# Marmota

A terminal file manager that works beside your Claude Code and Codex sessions. You start it with `y`.

By **Junjie Liang**. Marmota started from [superfile](https://github.com/yorukot/superfile) by
Yorukot and its contributors (MIT License), and has since been changed enough to be its own program:
it doesn't follow superfile's releases, and superfile's install scripts and release notes don't apply
to it.

## What it does

- **Files**: panels, previews, copy, move, rename, trash, archives, and `s` to go anywhere: pinned folders, sessions, folders zoxide knows, files edited lately.
- **Sessions beside your files.** Claude Code and Codex sessions run in WezTerm panes next to
  Marmota. The Sessions box at the foot (`!`) lists the running ones; each row says `cc`
  (Claude Code) or `co` (Codex), how it stands (working, needs you, replied) and what it is doing.
  The preview above shows the selected session's conversation.
- **Ask box** (`K`): write to the selected session, or start a new one in the background in the
  folder you are in. `tab` picks Claude Code or Codex for a new session, `ctrl+o` and `ctrl+t` the
  model and effort. A new session starts as Claude Code, its default model, medium effort.
- **Answering from the list**: when a session asks something (a permission, a question), the preview
  shows it and `K` hands your keys to it.
- **What a session changed** (`W`): the files it edited, and each one's diff in the preview.
- **Agent mode** (`>`): `claude`, `claude rc` (Remote Control) or `codex` opens a session in a pane
  beside Marmota.
- **`@`** types the selected files into the session beside Marmota as mentions.

## Requirements

- macOS (Linux builds; the session features need WezTerm)
- [WezTerm](https://wezterm.org)
- [Go](https://go.dev/doc/install) to build
- [Claude Code](https://claude.com/claude-code) and/or the [Codex CLI](https://github.com/openai/codex)
  for the session features. With Codex, Marmota talks to the server Codex runs for its windows.

## Build and install

```bash
git clone https://github.com/junjieliang672/marmota.git
cd marmota
./build.sh
```

On a Mac where Marmota isn't installed yet, `build.sh` installs everything first: Go, WezTerm, the
previews' tools, a Nerd Font, the `y` command, WezTerm's keys and look, and the editor Marmota opens
files in (see [INSTALL-DEPS.md](./INSTALL-DEPS.md)). After that it only builds; `./build.sh --install`
installs again, and `./build.sh --no-install` never does.

Every run of `build.sh`:

- builds the program into `bin/y`;
- keeps a block in your shell config (zsh, bash or fish) that adds `bin` to your PATH and defines a
  `y` function, which runs it and, with `cd_on_quit` on, moves your shell to the folder it quit in;
- keeps Marmota's look in `~/.wezterm.lua`: the pane you type in stays bright, the lines between panes
  are blue. It is one marked block; the rest of your WezTerm
  settings stay as they are;
- adds any setting a new version introduced to `~/.config/marmota`.

Each file it changes is backed up next to it first. Then, in a new terminal:

```bash
y
```

## Keys

| Key | What it does |
| --- | --- |
| `!` | Sessions box: in and out |
| `]` · `[` | Sessions box, on the next · previous session (after `W`: the diff's next · previous change) |
| `K` | Ask box: write to the selected session (or `n` in the Sessions box for a new one) |
| `tab` · `ctrl+o` · `ctrl+t` · `shift+tab` | In the Ask box: Claude Code or Codex · model · effort · permission mode |
| `>` | Agent mode: `claude`, `claude rc`, `codex` |
| `:` | Run a shell command in the current folder |
| `@` | Send the selected files to the session beside Marmota |
| `W` | The files the selected session changed |
| `V` | Pin the preview to a session while you browse |
| `w` · `r` · `x` · `m` | In the Sessions box: fork, rename, stop, mention a session |
| `s` | Go to: pinned, sessions (running or ended, to resume), folders zoxide knows, recent files |
| `ctrl+r` | In the Ask box, writing to a new session: resume an ended one instead |
| `M` · `F` | Focus the Details box · hide or show the boxes at the foot |
| `?` | All keys |
| `q` | Quit |

Keys are set in `~/.config/marmota/hotkeys.toml`; `?` lists them all.

## Configuration

`y path-list` prints where the configuration, hotkeys and logs are. Session settings in
`config.toml` include `codex_command` (the `codex` to run; blank picks the newest that works, from
PATH or the ChatGPT app).

## Documentation

New to it? [Getting started with Marmota](https://junjieliang672.github.io/marmota/tutorial.html) is a short tour (its source is
[docs/tutorial.html](./docs/tutorial.html)). In Marmota, `?` lists every key.

## License and credits

MIT License, see [LICENSE](./LICENSE): Copyright (c) 2026 Junjie Liang, and Copyright (c) 2024
Yorukot for superfile, which Marmota is adapted from. Thanks to Yorukot and superfile's
contributors for the file manager it is built on.
