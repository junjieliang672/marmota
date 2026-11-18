#!/usr/bin/env python3
"""Live test for micro as Marmota's editor: its keys, and Marmota's plugin.

Runs the real micro, with the settings, keys and plugin from scripts/micro,
in a tmux server of its own, and fake `claude` sessions in another (as
tmux_sessions_test.py does), reached through the real `y session`. Keys are
sent as the bytes WezTerm sends micro for them (setup-macos-hotkeys.sh's
block in ~/.wezterm.lua). Checks:

  1. The Mac keys: line and word moves, selecting by word, line and
     paragraph, delete to the line's start, undo and redo.
  2. cmd+G goes to a line, the file's top and bottom, the paragraph after
     and before, a bracket's match, and back; g keeps it open until esc;
     cmd+shift+G selects up to the same places, and what brackets hold.
     The wheel, turned while the left button drags, scrolls and the
     selection goes on.
  3. cmd+L asks the session the editor was opened for, about the file and
     the selected lines; typing goes on; the reply shows on the right;
     cmd+enter puts it at the cursor and cmd+Z takes it back.
  4. cmd+shift+L lists the sessions; a second session is asked too, and
     both replies show under their questions.
  5. The list's first row starts a session with the question; the next
     question goes on with it.
  6. cmd+K has the session change the selected lines in the file itself:
     the editor shows the change, and cmd+Z takes it back.
  7. A session on "another computer" (this one, through a command) gets the
     selected text itself, which it couldn't read from the file.

Nothing of yours is touched: micro's config, Marmota's state and the tmux
servers are all the test's own, and everything is removed at the end.

Usage: python3 testsuite/live/micro_plugin_test.py --binary ./bin/y
"""

from __future__ import annotations

import argparse
import json
import os
import shutil
import subprocess
import sys
import tempfile
import time
from pathlib import Path

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent

# What WezTerm sends micro for each key (wezterm_look_block), and what an
# unbound key sends by itself.
KEY = {
    "cmd+left": b"\x01", "cmd+right": b"\x05", "cmd+backspace": b"\x15", "opt+backspace": b"\x17",
    "opt+right": b"\x1bf", "shift+opt+right": b"\x1b[1;4C", "shift+opt+left": b"\x1b[1;4D",
    "shift+cmd+right": b"\x1b[1;6C", "shift+cmd+left": b"\x1b[1;6D", "cmd+down": b"\x1b[1;5B",
    "shift+opt+down": b"\x1b[1;4B", "cmd+a": b"\x1ba", "cmd+z": b"\x1a", "cmd+shift+z": b"\x19",
    "cmd+g": b"\x1bg", "cmd+shift+g": b"\x1bG", "cmd+s": b"\x13", "esc": b"\x1b", "right": b"\x1b[C",
    "cmd+l": b"\x1bl", "cmd+shift+l": b"\x1bL", "cmd+k": b"\x1bk",
    "cmd+enter": b"\x1b\r", "cmd+j": b"\x1bj", "enter": b"\r", "down": b"\x1b[B", "shift+down": b"\x1b[1;2B",
    "cmd+h": b"\x1bOP", "cmd+w": b"\x11", "cmd+/": b"\x1b/", "opt+n": b"\x1bn", "opt+]": b"\x1b]",
    "opt+m": b"\x1bm", "opt+x": b"\x1bx",
}
PLAN = "# Plan\n\nline three\nline four\nline five\n\nlast para one\nlast para two\n"
BRACKETS = "# Plan\n\nline three\nsay (a [b] c) d\n\nlast para one\nlast para two\n"
ROWS = "".join(f"row {n}\n" for n in range(1, 61))


class Micro:
    """micro in a tmux server of its own."""

    def __init__(self, server: str, cwd: Path, env: dict[str, str], config: Path, file: str) -> None:
        self.server = server
        self.tmux("kill-server")
        words = ["env", "-u", "SSH_CONNECTION", "-u", "TMUX", "-u", "TMUX_PANE", "-u", "WEZTERM_PANE",
                 *[f"{k}={v}" for k, v in env.items()], "micro", "-config-dir", str(config), file]
        self.tmux("-f", "/dev/null", "new-session", "-d", "-s", "t", "-x", "150", "-y", "24", "-c", str(cwd),
                  " ".join("'" + w.replace("'", "'\\''") + "'" for w in words))
        wait_for("micro to start", lambda: file in self.screen())

    def tmux(self, *args: str) -> str:
        return subprocess.run(["tmux", "-L", self.server, *args], capture_output=True, text=True).stdout

    def press(self, *names: str) -> None:
        for name in names:
            self.tmux("send-keys", "-t", "t", "-H", *[f"{b:02x}" for b in KEY[name]])
            time.sleep(0.15)

    def mouse(self, button: int, x: int, y: int, release: bool = False) -> None:
        """A mouse event as a terminal reports it (SGR): 0 the left button, +32 moved while held, 64 and 65
        the wheel up and down; x and y count from 1."""
        report = f"\x1b[<{button};{x};{y}{'m' if release else 'M'}".encode()
        self.tmux("send-keys", "-t", "t", "-H", *[f"{b:02x}" for b in report])
        time.sleep(0.15)

    def type(self, text: str) -> None:
        self.tmux("send-keys", "-t", "t", "-l", text)
        time.sleep(0.15)

    def screen(self) -> str:
        return self.tmux("capture-pane", "-p", "-t", "t")

    def left(self) -> list[str]:
        """The lines of the file's pane, without the line numbers."""
        out = []
        for line in self.screen().splitlines()[:-2]:
            line = line.split("|")[0] if "|" in line else line
            out.append(line[3:].rstrip())
        return out

    def right(self) -> str:
        """What the replies' pane shows."""
        return "\n".join(line.split("|", 1)[1] for line in self.screen().splitlines() if "|" in line)

    def quit(self) -> None:
        self.tmux("kill-server")


def wait_for(what: str, check, timeout: float = 15.0):
    deadline = time.time() + timeout
    while time.time() < deadline:
        got = check()
        if got:
            return got
        time.sleep(0.2)
    raise AssertionError(f"timed out waiting for: {what}")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", required=True)
    parser.add_argument("--keep", action="store_true")
    args = parser.parse_args()
    for tool in ("tmux", "micro", "fzf"):
        if not shutil.which(tool):
            print(f"SKIP: no {tool}")
            return 0

    y = str(Path(args.binary).resolve())
    work = Path(tempfile.mkdtemp(prefix="spf-micro-", dir="/private/tmp"))
    project = work / "project"
    project.mkdir()
    (work / "home").mkdir()
    plan = project / "plan.md"
    socket = f"marmota-micro-{os.getpid()}"
    server = f"micro-test-{os.getpid()}"
    env = {
        "PATH": f"{HERE / 'fakes'}:{os.environ['PATH']}",
        "FAKE_CLAUDE_REG": str(work / "registry"),
        "CLAUDE_CONFIG_DIR": str(work / "claude-home"),
        "XDG_DATA_HOME": str(work / "iso" / "data"),
        "XDG_STATE_HOME": str(work / "iso" / "state"),
        "XDG_CACHE_HOME": str(work / "iso" / "cache"),
        "MARMOTA_TMUX_SOCKET": socket,
        "HOME": str(work / "home"),
        "TERM": "xterm-256color",
    }
    env_args = ["env", "-u", "SSH_CONNECTION", "-u", "TMUX", "-u", "TMUX_PANE", "-u", "WEZTERM_PANE",
                *[f"{k}={v}" for k, v in env.items()]]

    def session(*a: str) -> str:
        return subprocess.run([*env_args, y, "session", *a], check=True, capture_output=True, text=True,
                              cwd=project).stdout

    def sent_to(socket_name: str, session_id: str) -> str:
        """What a fake session's pane shows: what was sent to it."""
        return subprocess.run(["tmux", "-L", socket_name, "capture-pane", "-p", "-J", "-t", "cc-" + session_id],
                              capture_output=True, text=True).stdout

    # micro's config: Marmota's settings, keys and plugin, and no clipboard
    # of yours.
    config = work / "micro"
    (config / "plug").mkdir(parents=True)
    shutil.copy(REPO / "scripts/micro/bindings.json", config)
    settings = json.loads((REPO / "scripts/micro/settings.json").read_text())
    # each editor of the test starts at the top, with nothing to undo
    settings.update({"clipboard": "internal", "savecursor": False, "saveundo": False})
    (config / "settings.json").write_text(json.dumps(settings))
    shutil.copytree(REPO / "scripts/micro/marmota", config / "plug/marmota")

    def editor(session_id: str = "", title: str = "", host: str = "", text: str = PLAN) -> Micro:
        plan.write_text(text)
        shutil.rmtree(config / "backups", ignore_errors=True)
        shutil.rmtree(config / "buffers", ignore_errors=True)
        return Micro(server, project, {**env, "MARMOTA_BIN": y, "MARMOTA_SESSION": session_id,
                                       "MARMOTA_SESSION_TITLE": title, "MARMOTA_SESSION_HOST": host}, config,
                     "plan.md")

    m = None
    try:
        # 1. the Mac keys
        def after(*steps: str, text: str = PLAN) -> list[str]:
            ed = editor(text=text)
            for step in steps:
                if step in KEY:
                    ed.press(step)
                else:
                    ed.type(step)
            time.sleep(0.2)
            lines = ed.left()
            ed.quit()
            return lines

        assert after("cmd+right", "!")[0] == "# Plan!", "cmd+right goes to the line's end"
        assert after("cmd+right", "cmd+left", "!")[0] == "!# Plan", "cmd+left goes to its start"
        assert after("cmd+right", "opt+backspace")[0] == "#", "option+backspace deletes a word"
        assert after("cmd+right", "cmd+backspace")[0] == "", "cmd+backspace deletes to the line's start"
        assert after("down", "down", "cmd+left", "cmd+backspace", "cmd+backspace")[0] == "# Planline three", \
            "at a line's start it joins the lines"
        assert after("down", "down", "shift+opt+right", "X")[2] == "X three", "shift+option+right selects a word"
        assert after("down", "down", "shift+cmd+right", "X")[2] == "X", "shift+cmd+right selects to the line's end"
        assert after("down", "down", "shift+opt+down", "X")[2:4] == ["X", ""], "shift+option+down selects the paragraph"
        assert after("cmd+a", "X")[0] == "X", "cmd+A selects all"
        assert after("zzz", "cmd+z")[0] == "# Plan", "cmd+Z undoes"
        assert after("zzz", "cmd+z", "cmd+shift+z")[0] == "zzz# Plan", "cmd+shift+Z redoes"
        assert after("cmd+down", "END")[-1].endswith("END") or "END" in "".join(after("cmd+down", "END")), \
            "cmd+down goes to the file's end"
        print("ok   the Mac keys move, select and delete as in a text field; undo and redo work")

        # 2. cmd+G goes to a place, cmd+shift+G selects up to it
        assert after("cmd+g", "4", "enter", "!")[3] == "!line four", "cmd+G, a number: that line"
        assert after("cmd+g", "b", "!")[8] == "!", "b: the file's bottom"
        assert after("cmd+g", "b", "cmd+g", "t", "!")[0] == "!# Plan", "t: its top"
        assert after("cmd+g", "p", "!")[2] == "!line three", "p: the next paragraph's start"
        assert after("cmd+g", "p", "cmd+g", "p", "cmd+g", "P", "!")[2] == "!line three", "P: the one before"
        assert after("down", "down", "down", "cmd+g", "P", "!")[2] == "!line three", "P inside one: its own start"
        assert after("cmd+g", "g", "p", "p", "P", "esc", "!")[2] == "!line three", \
            "g keeps the line open for more keys, until esc"
        assert after("cmd+g", "g", "p", "p", "n", "esc", "!")[2] == "!line three", "n: back to where a jump left from"
        assert after("cmd+g", "g", "p", "9", "enter", "4", "enter", "esc", "!")[3] == "!line four", \
            "numbers too while it stays open"
        inside = ["down"] * 3 + ["right"] * 8  # on the b of "say (a [b] c) d"
        assert after(*inside, "cmd+g", "m", "!", text=BRACKETS)[3] == "say (a ![b] c) d", \
            "m: the bracket that opens around the cursor"
        assert after(*inside, "cmd+g", "g", "m", "m", "esc", "!", text=BRACKETS)[3] == "say (a [b!] c) d", \
            "m on a bracket: its match"
        assert after("down", "down", "cmd+shift+g", "4", "enter", "X")[2:4] == ["X", "line five"], \
            "cmd+shift+G, a number: selects through that line"
        assert after("down", "down", "cmd+shift+g", "t", "X")[0] == "Xline three", "t selects to the top"
        assert after("down", "down", "cmd+shift+g", "b", "X")[2:4] == ["X", ""], "b to the bottom"
        assert after("down", "down", "right", "cmd+shift+g", "p", "X")[2:4] == ["lX", ""], \
            "p to the paragraph's end"
        assert after("down", "down", "right", "cmd+shift+g", "g", "p", "p", "esc", "X")[2:4] == ["lX", ""], \
            "again, the next one's"
        assert after("down", "down", "down", "right", "cmd+shift+g", "P", "X")[2] == "Xine four", \
            "P to the paragraph's start"
        assert after(*inside, "cmd+shift+g", "m", "X", text=BRACKETS)[3] == "say (a [X] c) d", \
            "m selects what the brackets hold"
        assert after(*inside, "cmd+shift+g", "g", "m", "m", "esc", "X", text=BRACKETS)[3] == "say (a X c) d", \
            "again, the brackets too"
        assert after(*inside, "cmd+shift+g", "g", "m", "m", "m", "esc", "X", text=BRACKETS)[3] == "say (X) d", \
            "and again, what the pair around them holds"
        # the wheel while the left button drags: twelve lines down, four back up
        m = editor(text=ROWS)
        m.mouse(0, 12, 3)
        m.mouse(32, 12, 4)
        for _ in range(6):
            m.mouse(65, 12, 4)
        for _ in range(2):
            m.mouse(64, 12, 4)
        m.mouse(0, 12, 4, release=True)
        m.type("X")
        m.press("cmd+s")
        wait_for("the file saved", lambda: "row 3X\n" in plan.read_text())
        assert plan.read_text().startswith("row 1\nrow 2\nrow 3X\nrow 13\n"), \
            "the wheel scrolled while dragging, and the selection went on to the pointer: " + plan.read_text()[:40]
        m.quit()
        m = None
        print("ok   cmd+G went to lines, the top and bottom, paragraphs, brackets and back, staying open after g; "
              "cmd+shift+G selected up to them; the wheel scrolled a drag's selection on")

        # 3. the page of keys, closing, comments, several cursors
        assert after("cmd+/")[0] == "<!-- # Plan -->", "cmd+/ comments the line"
        assert after("cmd+/", "cmd+/")[0] == "# Plan", "and again takes the comment off"
        assert after("down", "down", "opt+n", "opt+n", "X")[2:4] == ["X three", "X four"], \
            "option+N selects the word, then its next occurrence too, and typing goes to both"
        assert after("cmd+g", "5", "enter", "!", "cmd+g", "t", "opt+]", "?")[4] == "?!line five", \
            "option+] goes to the next changed line"
        assert after("down", "down", "shift+down", "shift+down", "opt+m", "X")[2:4] == ["Xline three", "Xline four"], \
            "option+M puts a cursor on each selected line"
        assert after("opt+x", "replace line row -a", "enter")[2:5] == ["row three", "row four", "row five"], \
            "option+X opens the command bar, and replace with -a replaces all"
        m = editor()
        m.press("cmd+h")
        wait_for("the page of keys", lambda: "Keys in micro" in m.screen())
        m.press("cmd+h")
        wait_for("cmd+H to put the page away", lambda: "Keys in micro" not in m.screen())
        m.press("cmd+h")
        wait_for("the page of keys again", lambda: "Keys in micro" in m.screen())
        m.press("cmd+w")
        wait_for("cmd+W to close the page", lambda: "Keys in micro" not in m.screen())
        assert "# Plan" in m.screen(), "the file stays open"
        m.type("zz")
        m.press("cmd+w")
        wait_for("cmd+W to ask about what isn't saved", lambda: "Save changes" in m.screen())
        m.type("n")
        wait_for("micro to close", lambda: m.screen().strip() == "")
        assert plan.read_text() == PLAN, "nothing was saved"
        m = None
        print("ok   cmd+H showed the page of keys and put it away; cmd+W closed the page, then micro, asking "
              "about what wasn't saved; cmd+/ commented, option+N and option+M typed at several cursors, "
              "option+] found the change, the command bar replaced all")

        # two sessions to ask
        first = session("start", "--cwd", str(project), "--", "hello").split()[1]
        second = session("start", "--cwd", str(project), "--", "second").split()[1]
        wait_for("the sessions", lambda: session("list").count(" idle ") == 2)

        # 4. cmd+L asks the editor's session about the file and the selected lines
        m = editor(first, "first", "here")
        assert "→ first" in m.screen().splitlines()[-2], "the status line says whom questions go to"
        m.press("down", "down", "shift+down", "shift+down", "cmd+l")
        wait_for("the question's prompt", lambda: "Ask first ›" in m.screen())
        m.type("make these shorter")
        m.press("enter")
        m.press("cmd+down")
        m.type("typed while it works ")
        wait_for("the reply on the right", lambda: "Echo: make these shorter" in m.right())
        assert "› make these shorter" in m.right() and "first" in m.right()
        wait_for("the question in the session", lambda: f"@{plan}#L3-4" in sent_to(socket, first))
        assert any("typed while it works" in line for line in m.left()), "typing went on meanwhile"
        m.press("cmd+enter")
        wait_for("the reply at the cursor", lambda: any(
            line.endswith("typed while it works Echo: make these shorter") for line in m.left()))
        m.press("cmd+z")
        wait_for("it taken back", lambda: not any("Echo" in line for line in m.left()))
        print("ok   cmd+L asked the session about the file and lines 3-4 while typing went on; its reply showed "
              "on the right; cmd+enter put it at the cursor and cmd+Z took it back")

        # 5. another session, picked from the list
        m.press("cmd+shift+l")
        wait_for("the list of sessions", lambda: "New session in project" in m.screen() and "idle" in m.screen())
        assert first not in m.screen(), "the ids stay out of sight"
        m.press("down", "down", "enter")  # the row of the second session
        wait_for("the question's prompt", lambda: "Ask " in m.screen().splitlines()[-1])
        m.type("and you")
        m.press("enter")
        wait_for("its reply", lambda: "Echo: and you" in m.right())
        assert "Echo: make these shorter" in m.right(), "the first reply stays above"
        wait_for("the question in the second session", lambda: "and you" in sent_to(socket, second))
        print("ok   cmd+shift+L listed the sessions and a new one; the second was asked too, and both replies "
              "show under their questions")

        # 6. a session the question starts
        m.press("cmd+shift+l")
        wait_for("the list of sessions", lambda: "New session in project" in m.screen())
        m.press("enter")  # the first row
        wait_for("the question's prompt", lambda: "Ask new · project ›" in m.screen())
        m.type("start with this")
        m.press("enter")
        wait_for("the new session's reply", lambda: "Ready." in m.right())
        started = [line.split()[0] for line in session("list").splitlines()[1:] if line.split()[0] not in (first, second)]
        assert len(started) == 1, session("list")
        m.press("cmd+l")
        wait_for("the question's prompt", lambda: "Ask new · project ›" in m.screen())
        m.type("a follow-up")
        m.press("enter")
        wait_for("the follow-up's reply", lambda: "Echo: a follow-up" in m.right())
        wait_for("the follow-up in the new session", lambda: "a follow-up" in sent_to(socket, started[0]))
        print("ok   the list's first row started a session with the question, and the next question went on "
              "with it")
        m.quit()

        # 7. cmd+K: the session changes the lines in the file
        m = editor(first, "first", "here")
        m.press("down", "down", "shift+down", "shift+down", "cmd+k")
        wait_for("the change's prompt", lambda: "first, change these lines ›" in m.screen())
        m.type("in capitals")
        m.press("enter")
        wait_for("the change in the editor", lambda: m.left()[2:5] == ["LINE THREE", "LINE FOUR", "line five"])
        assert plan.read_text().splitlines()[2:4] == ["LINE THREE", "LINE FOUR"], "the file itself changed"
        assert "Put lines 3-4 in capitals." in m.right()
        assert "changed plan.md" in m.screen().splitlines()[-1]
        m.press("cmd+enter")
        time.sleep(0.5)
        assert "Put lines" not in "\n".join(m.left()), "what it said of its change isn't a reply to put in"
        m.press("cmd+z")
        wait_for("the change taken back", lambda: m.left()[2:4] == ["line three", "line four"])
        print("ok   cmd+K had the session change lines 3-4 in the file; the editor showed them and cmd+Z took "
              "the change back")
        m.quit()

        # 8. a session on another computer gets the text itself
        remote_socket = socket + "-remote"
        on = f"env MARMOTA_TMUX_SOCKET={remote_socket} /bin/sh -c"
        out = session("start", "--on", on, "--cwd", str(project), "--", "far away")
        remote_id, host = out.split()[1], out.split(" on ")[1].split(" in ")[0]
        wait_for("the remote session", lambda: any(
            line.split()[0] == remote_id and " idle " in line for line in session("list").splitlines()[1:]), timeout=30)
        m = editor(remote_id, "far", host)
        m.press("down", "down", "shift+cmd+right", "cmd+l")
        wait_for("the question's prompt", lambda: "Ask far ›" in m.screen())
        m.type("what is this")
        m.press("enter")
        wait_for("its reply", lambda: "Echo: what is this" in m.right(), timeout=30)
        got = sent_to(remote_socket, remote_id)
        assert "From plan.md:" in got and "line three" in got and "@" + str(plan) not in got, got
        print(f"ok   a session on {host} was sent the selected text itself, and its reply came back")
        print("PASS")
        return 0
    except AssertionError:
        if m is not None:
            print("--- the editor's screen when it failed:\n" + m.screen().rstrip())
        raise
    finally:
        if m is not None:
            m.quit()
        if not args.keep:
            for name in (socket, socket + "-remote", server):
                subprocess.run(["tmux", "-L", name, "kill-server"], capture_output=True)
            shutil.rmtree(work, ignore_errors=True)


if __name__ == "__main__":
    sys.exit(main())
