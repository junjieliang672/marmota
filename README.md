# Marmota

A terminal file manager that works beside your Claude Code and Codex sessions. You start it with `y`.

By **Junjie Liang**. Marmota started from [superfile](https://github.com/yorukot/superfile) by
Yorukot and its contributors (MIT License), and has since been changed enough to be its own program:
it doesn't follow superfile's releases, and superfile's install scripts and release notes don't apply
to it.

**Website:** [junjieliang672.github.io/marmota](https://junjieliang672.github.io/marmota/) ·
**Tutorial:** [Getting started with Marmota](https://junjieliang672.github.io/marmota/tutorial.html)

## What it does

- **Files**: panels, previews, copy, move, rename, trash, archives, and `s` to go anywhere: pinned folders, sessions, folders zoxide knows, files edited lately.
- **Sessions beside your files.** The Claude Code and Codex sessions Marmota starts run in tmux
  (a server of Marmota's own, out of sight), so they keep running when you close their pane, quit
  Marmota or quit WezTerm: run `y` again and they are all there. A pane beside Marmota shows the
  one you pick. The Sessions box at the foot (`!`) lists the running ones; each row says `cc`
  (Claude Code) or `co` (Codex), how it stands (working, needs you, replied) and what it is doing.
  The preview above shows the selected session's conversation.
- **Sessions on other computers.** A session started on another computer over ssh (or a company
  CLI) runs in Marmota's tmux there and shows in the Sessions box with a cloud, named
  `computer · title` until you rename it with `r`. You write to it, answer it and open it beside
  Marmota as one on this computer. When the computer can't be reached, its sessions stay listed as
  last seen, and `enter` on one logs in. See [Sessions on other computers](#sessions-on-other-computers).
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
- [tmux](https://github.com/tmux/tmux), where sessions run (`build.sh` installs it). Without it,
  sessions run in WezTerm panes and end with WezTerm.
- [Go](https://go.dev/doc/install) to build
- [Claude Code](https://claude.com/claude-code) and/or the [Codex CLI](https://github.com/openai/codex)
  for the session features. With Codex, Marmota talks to the server Codex runs for its windows.
- For sessions on another computer: a way to reach it that runs a command there (`ssh`, or a
  company CLI), and tmux and Claude Code installed there. Marmota itself needn't be.

## Build and install

```bash
git clone https://github.com/junjieliang672/marmota.git
cd marmota
./build.sh
```

On a Mac where Marmota isn't installed yet, `build.sh` installs everything first: Go, tmux, WezTerm,
the previews' tools, a Nerd Font, the `y` command, WezTerm's keys and look, and the editor Marmota opens
files in (see [INSTALL-DEPS.md](./INSTALL-DEPS.md)). After that it only builds; `./build.sh --install`
installs again, and `./build.sh --no-install` never does.

Every run of `build.sh`:

- builds the program into `bin/y`;
- keeps a block in your shell config (zsh, bash or fish) that adds `bin` to your PATH and defines a
  `y` function, which runs it and, with `cd_on_quit` on, moves your shell to the folder it quit in;
- keeps Marmota's look in `~/.wezterm.lua`: the pane you type in stays bright, the lines between panes
  are blue. It is one marked block; the rest of your WezTerm
  settings stay as they are;
- adds any setting a new version introduced to `~/.config/marmota`;
- installs the `marmota-sessions` skill into `~/.claude/skills` (and `~/.codex/skills` when Codex is
  set up), which tells a coding agent about `y session`.

Each file it changes is backed up next to it first. Then, in a new terminal:

```bash
y
```

## Keys

| Key | What it does |
| --- | --- |
| `!` | Sessions box, on a session that needs you (again: the next one), else the first; `esc` goes back |
| `]` · `[` | Sessions box, on the next · previous session (after `W`: the diff's next · previous change) |
| `K` | Ask box: write to the selected session (or `n` in the Sessions box for a new one) |
| `tab` · `ctrl+o` · `ctrl+t` · `shift+tab` | In the Ask box: Claude Code or Codex · model · effort · permission mode |
| `shift+enter` | In the Ask box: a new line (the WezTerm setup sends it as `ctrl+j`, which works as well) |
| `ctrl+g` | In the Ask box: write the question in your editor, as in Claude Code; closing it brings what you wrote back, unsent. A long question shows folded, `[first words… +11 lines]` |
| drag | In the session preview: select text; letting go copies it |
| `e` | Open the file in the editor, in Marmota's place until you close it; with `editor_beside = true`, in a pane on Marmota's right instead. The editor is micro, one without modes where the Mac's keys (cmd+S, cmd+Z, cmd+←, shift+option+→ …) work as in a text field: on a Mac `./build.sh` installs and sets it up, also as `$EDITOR`, so Claude Code's ctrl+G opens it too; `E` opens it in a new tab |
| cmd+L (in micro) | Ask a session about the file you are writing, with the lines you selected: its reply shows on the right, cmd+enter puts it at the cursor. cmd+shift+L picks another session, Claude Code's or Codex's, or starts one of either; cmd+K has the session change the selected lines in the file |
| cmd+H, cmd+W (in micro) | cmd+H shows the page of keys worth knowing, micro's and Marmota's, and puts it away. cmd+W closes the file, tab or pane the cursor is in, and the last one micro itself (ctrl+Q does too). cmd+/ comments a line. Elsewhere these keys do what they did: cmd+H hides WezTerm, cmd+W closes its tab |
| cmd+G (in micro) | Go to a place: a line's number and enter, or one key: `t` top, `b` bottom, `p`/`P` the next paragraph or the one before, `m` the matching bracket, `n` back. `g` keeps it open for more, until esc. cmd+shift+G selects up to the same places, and with `m` what the brackets hold |
| `>` | Agent mode: `claude`, `claude rc`, `codex` |
| `:` | Run a shell command in the current folder |
| `@` | Send the selected files to the session beside Marmota |
| `W` | The files the selected session changed |
| `V` | Pin the preview to a session while you browse |
| `w` · `r` · `x` · `m` | In the Sessions box: fork, rename, stop, mention a session |
| `s` or `z` | Go to: pinned, sessions (running or ended, to resume), folders zoxide knows, recent files; `enter` on a running session puts the Sessions box on it and leaves the pane beside Marmota as it is |
| `ctrl+r` | In the Ask box, writing to a new session: resume an ended one instead |
| `M` · `F` | Focus the Details box · hide or show the boxes at the foot |
| `?` | All keys |
| `q` | Quit |

Keys are set in `~/.config/marmota/hotkeys.toml`; `?` lists them all.

## Sessions on other computers

You don't set anything up: a coding agent in one of your sessions starts one, and Marmota remembers
the computer from then on and keeps one connection to it. Ask it in your own words ("open a session
on my Mac mini in ~/proj that fixes the flaky test"); the `marmota-sessions` skill tells it to run:

```bash
y session start --on "ssh me@192.168.1.20" --cwd ~/proj -- "fix the flaky test"
y session start --agent co -- "review the diff"   # a Codex session instead, on this computer
y session list                       # every session, here and there, who runs it and what each is doing
y session send <id> "and add a test" # type into its prompt and send it
y session read <id> --turns 3        # its last turns
```

`--on` is how you would reach the computer from a terminal: `"ssh me@host"`, `"ssh -J jump me@host"`,
or a company CLI that runs a command there, such as `"corp ssh gpu-1 --"`. With ssh, Marmota shares
one connection for everything (ControlMaster), so you log in once. A session there runs with Remote
Control, so the Claude app has it too. On a Mac reached over ssh, Marmota starts its tmux there in the
logged-in user's session, so Claude Code there is signed in as usual.

Sessions someone already started on that computer, outside Marmota, move in with `y session adopt`:

```bash
y session adopt --on "ssh me@192.168.1.20" --list   # what runs there outside Marmota
y session adopt --on "ssh me@192.168.1.20" <name>   # move it in; no name moves them all
```

Each one stops where it ran (its terminal goes back to its shell, or closes when it ran nothing else)
and goes on with the same conversation, permission mode, model and Remote Control in Marmota's tmux.

If a computer needs you to log in again, say when a sign-in expires, its sessions stay in the
Sessions box, greyed, with "needs login": `enter` on one opens a pane beside Marmota where ssh (or the
company CLI's login) asks what it asks, and Marmota reaches the computer again once you're done.

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
