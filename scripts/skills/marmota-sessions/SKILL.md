---
name: marmota-sessions
description: Start, list, write to and read Claude Code sessions on this computer or on another one (over ssh or a company CLI), and Codex sessions on this computer, through Marmota's `y session` command. Use when the user asks to open, start or run a session (or agent) on another machine or in a folder, to check what such sessions are doing, to send one a message, or to read its reply.
---

# Sessions through Marmota

Marmota (the `y` command) runs Claude Code sessions in tmux, on this computer
or another one, so they keep running when the terminal or the connection goes
away. What you start here shows in the user's Marmota Sessions box, where they
can watch it, answer it and open it beside Marmota.

## Start a session

```
y session start [--agent cc|co] [--on "<how to reach the computer>"] [--cwd <folder>] \
    [--model <model>] [--effort <effort>] [--permission-mode <mode>] -- "<first message>"
```

- `--agent co` starts a Codex session instead of a Claude Code one (`cc`, the
  default). Codex sessions start on this computer only: no `--on`. Its effort
  is medium unless you give one, and it runs in Codex's nearest to the
  permission mode.

- `--on` is the command that reaches the other computer, as the user would type
  it: `"ssh me@192.168.1.20"`, `"ssh -J jump.corp me@gpu-1"`, or a company CLI
  that runs a command there, such as `"corp ssh gpu-1 --"`. Leave it out for
  this computer. Marmota remembers the computer from then on.
- `--cwd` is the folder over there; `~/Developer/proj` is under that
  computer's home. On another computer it defaults to its home, here to the
  current folder.
- `--permission-mode` defaults to bypassPermissions, as the Ask box starts a
  session. Use `default` when the user wants to approve each tool.
- A session on another computer runs with Remote Control, so the Claude app
  has it too.

It prints the session's id. If the other computer needs a login (a password,
MFA or browser sign-in), the command fails and says so: tell the user to log
in there once in a terminal, then try again. Never ask for or type their
password or codes.

## Bring in sessions already running there

```
y session adopt --on "<how to reach the computer>" --list   # the sessions there outside Marmota
y session adopt --on "<how to reach the computer>" [<name or id>...]
```

Moves Claude Code sessions someone started on that computer outside Marmota
into it: each one's process stops where it runs (its terminal goes back to
its shell, or closes when the session was all it ran) and it goes on with the
same id and conversation, permission mode, model, effort and Remote Control
in Marmota's tmux there. Without names it moves all of them, whatever they are
doing, so run `--list` first and tell the user what will move.

## Follow it

```
y session list [--json]          # every session, who runs it (cc Claude Code, co Codex),
                                 # where it runs, and its status
y session read <id> [--turns N]  # its last turns (default 3)
y session send <id> "<message>"  # send it a message (typed into its prompt; Codex takes
                                 # it through its own server)
y session ask <id> "<message>"   # send it, wait for that turn to end, print the reply
                                 # (--sent: only wait, for the message it was started with;
                                 #  exit 3: it stopped to ask the user something;
                                 #  --keep-waiting: wait for the user to answer it instead)
y session show <id>              # open its own window in a pane under yours (WezTerm), for
                                 # the user to watch it or answer what it asks
```

`y session start --temporary` starts a session for the program that runs it
alone: Marmota's Sessions box and `y session list` leave it out, and it closes
when that program exits. Marmota's editor starts one so for a question asked
from it. Don't use it for a session the user is to follow.

The id can be shortened to its first 8 characters. A status of `waiting`
means the session asks something (a permission or a question): tell the user,
who answers it from Marmota's Sessions box (K) or the pane beside Marmota;
don't answer a permission for them unless they asked you to.
