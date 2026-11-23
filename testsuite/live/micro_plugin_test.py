#!/usr/bin/env python3
"""Live test for micro as Marmota's editor: its keys, and Marmota's plugin.

Runs the real micro, with the settings, keys and plugin from scripts/micro,
in a tmux server of its own, and fake `claude` sessions in another (as
tmux_sessions_test.py does), reached through the real `y session`. Keys are
sent as the bytes WezTerm sends micro for them (setup-macos-hotkeys.sh's
block in ~/.wezterm.lua). Checks:

  1. The Mac keys: line and word moves, selecting by word, line and
     paragraph; in a line of Chinese the words are the system's, and
     option+backspace deletes what option+left goes over;
     paragraph, delete to the line's start, undo and redo.
  2. cmd+G goes to a line, the file's top and bottom, the paragraph after
     and before, a bracket's match, and back; g keeps it open until esc;
     cmd+shift+G selects up to the same places, and what brackets hold.
     The wheel, turned while the left button drags, scrolls and the
     selection goes on.
  3. cmd+L opens a column on the right and puts the cursor in the box at
     its foot: shift+enter is a new line there, esc goes back to the file
     with what is written kept, enter sends it to the session the editor
     was opened for, about the file and the selected lines; typing goes on
     in the file; the reply shows above the box; cmd+enter puts it at the
     cursor and cmd+Z takes it back.
  4. cmd+shift+L lists the sessions above the box, at once, the file still
     in view; what is typed in the box filters them, the arrows move the
     mark, esc leaves the list, enter picks. A second session is asked too,
     and both replies show under their questions.
  5. The list's first row starts a session with the question; the next
     question goes on with it.
  6. cmd+K has the box take what the session is to change in the selected
     lines, in the file itself: the editor shows the change, and cmd+Z
     takes it back.
  7. cmd+J puts the column away and shows it again with what the box held;
     cmd+L from the box goes back to the file; cmd+W in the box puts the
     column away; in another tab the column comes along with what was
     asked.
  8. A question names its file and lines: enter on it in the replies
     selects them in the file again, where they moved to when lines went in
     above; changed since, it says so. cmd+K's names the lines its change
     spans, and enter selects those; with the file closed, it opens again.
  9. The real Marmota opens micro with e; cmd+M puts micro away, as it is,
     and Marmota says so; e opens another micro, which says it can't be put
     away too; b brings the first back with what was typed; q brings it
     back while something isn't saved, and ends it and quits once it is.

Nothing of yours is touched: micro's config, Marmota's state and the tmux
servers are all the test's own, and everything is removed at the end.

Usage: python3 testsuite/live/micro_plugin_test.py --binary ./bin/y
"""

from __future__ import annotations

import argparse
import json
import os
import re
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
    "cmd+l": b"\x1bl", "cmd+shift+l": b"\x1bL", "cmd+k": b"\x1bk", "cmd+shift+c": b"\x1bC",
    "cmd+enter": b"\x1b\r", "cmd+j": b"\x1bj", "enter": b"\r", "down": b"\x1b[B", "shift+down": b"\x1b[1;2B",
    "cmd+h": b"\x1bOP", "cmd+w": b"\x11", "cmd+/": b"\x1b/", "opt+n": b"\x1bn", "opt+]": b"\x1b]",
    "opt+m": b"\x1bm", "opt+x": b"\x1bx", "cmd+o": b"\x1bOQ", "cmd+m": b"\x1bOR",
    "shift+enter": b"\n", "backspace": b"\x7f", "up": b"\x1b[A", "opt+,": b"\x1b,",
    "opt+left": b"\x1bb", "opt+delete": b"\x1b[3;3~", "opt+w": b"\x1bw",
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
        """What the column on the right shows: the replies or the sessions, then the box."""
        return "\n".join(line.split("|", 1)[1] for line in self.screen().splitlines() if "|" in line)

    def box(self) -> list[str]:
        """The lines written in the column's box, which is under the line that names its keys."""
        lines = self.right().splitlines()
        for i, line in enumerate(lines):
            if "enter sends" in line or "enter picks" in line:
                rows = [row.strip() for row in lines[i + 1:-1]]
                while rows and not rows[-1]:
                    rows.pop()
                return rows
        return []

    def foot(self) -> str:
        """The box's status line: what it is for."""
        lines = self.right().splitlines()
        return lines[-1].strip() if lines else ""

    def column(self) -> bool:
        """Whether a column shows beside the file: a line drawn down the screen, over the status lines."""
        return any("|" in line for line in self.screen().splitlines()[:-2])

    def in_box(self) -> bool:
        """Whether the cursor is in the column, which is right of the file's pane. micro shows no cursor
        while the pane with the keyboard has a selection, as the file's has with lines selected for a
        question: the cursor is then not in the box, which never has one."""
        for line in self.screen().splitlines()[:-2]:
            if "|" in line:
                shown, at = self.tmux("display", "-p", "-t", "t", "#{cursor_flag} #{cursor_x}").split()
                return shown == "1" and int(at) > len(line.split("|", 1)[0])
        return False

    def drawn(self, line_with: str, word: str, what: str = "") -> str:
        """How word is drawn on the screen's line that has line_with: the escape sequence just before the
        word, which sets its color; with what "48", the background set last before it."""
        for line in self.tmux("capture-pane", "-p", "-e", "-t", "t").splitlines():
            if line_with in re.sub(r"\x1b\[[0-9;]*m", "", line):
                if what:
                    before = line.split(word, 1)[0]
                    found = re.findall(r"\x1b\[(" + what + r";[0-9;]*)m", before)
                else:
                    found = re.findall(r"\x1b\[([0-9;]*)m" + re.escape(word), line)
                return found[-1] if found else ""
        return ""

    def quit(self) -> None:
        self.tmux("kill-server")


class Marmota(Micro):
    """The real Marmota in the tmux server, with micro as its editor: e opens it in Marmota's place."""

    def __init__(self, server: str, cwd: Path, env: dict[str, str], config: Path, y: str) -> None:
        self.server = server
        self.tmux("kill-server")
        words = ["env", "-u", "SSH_CONNECTION", "-u", "TMUX", "-u", "TMUX_PANE", "-u", "WEZTERM_PANE",
                 *[f"{k}={v}" for k, v in env.items()], "EDITOR=micro", f"MICRO_CONFIG_HOME={config}",
                 "MARMOTA_NO_IME_HOLD=1", y, str(cwd)]
        command = " ".join("'" + w.replace("'", "'\\''") + "'" for w in words)
        self.tmux("-f", "/dev/null", "new-session", "-d", "-s", "t", "-x", "150", "-y", "34", "-c", str(cwd),
                  command + "; echo MARMOTA-ENDED; sleep 60")
        wait_for("Marmota to start", lambda: "plan.md" in self.screen())
        self.press("esc")  # the first key of a first run only puts the welcome away

    def runs(self) -> str:
        """The program tmux says the pane runs: the terminal's foreground job, as WezTerm asks it."""
        return self.tmux("display", "-p", "-t", "t", "#{pane_current_command}").strip()


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
    work = Path(tempfile.mkdtemp(prefix="marmota-micro-", dir="/private/tmp"))
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
        # A line of Chinese has no spaces, and micro takes it for one word: the words are the system's,
        # which y asks, and deleting takes what moving goes over.
        zh = "我认为应该这样，enter 进入\n"
        assert after("cmd+right", "opt+left", "X", text=zh)[0] == "我认为应该这样，enter X进入", \
            "option+left goes to the start of the word before"
        assert after("cmd+right", "opt+left", "opt+left", "opt+left", "X", text=zh)[0] == "我认为应该X这样，enter 进入", \
            "and on, over the comma"
        assert after("opt+right", "opt+right", "X", text=zh)[0] == "我认为X应该这样，enter 进入", \
            "option+right goes to the end of the word after"
        assert after("cmd+right", "opt+backspace", "X", text=zh)[0] == "我认为应该这样，enter X", \
            "option+backspace deletes one word"
        assert after("cmd+right", "opt+backspace", "opt+backspace", "opt+backspace", text=zh)[0] == "我认为应该", \
            "and again, what option+left goes over each time"
        assert after("opt+delete", "opt+delete", text=zh)[0] == "应该这样，enter 进入", \
            "option+delete deletes the word after"
        assert after("cmd+right", "shift+opt+left", "shift+opt+left", "X", text=zh)[0] == "我认为应该这样，X", \
            "shift+option+left selects by the same words"
        assert after("opt+right", "shift+opt+right", "X", text=zh)[0] == "我X应该这样，enter 进入", \
            "and shift+option+right"
        assert after("cmd+right", "opt+left", "X", text="--flag 我认为\n")[0] == "--flag 我X认为", \
            "a line that starts as an option does is a line all the same"
        print("ok   the Mac keys move, select and delete as in a text field; undo and redo work; in a line of "
              "Chinese they go by the system's words, and deleting takes what moving goes over")

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
        # cmd+shift+C copies the file and lines as a session reads them,
        # to a pbcopy of the test's, not yours
        clip = work / "clip"
        clip.mkdir(exist_ok=True)
        (clip / "pbcopy").write_text(f"#!/bin/sh\ncat > '{work / 'clipboard'}'\n")
        (clip / "pbcopy").chmod(0o755)
        plan.write_text(PLAN)
        m = Micro(server, project, {**env, "PATH": f"{clip}:{env['PATH']}"}, config, "plan.md")
        m.press("down", "down", "shift+down", "shift+down", "cmd+shift+c")
        wait_for("the reference copied", lambda: (work / "clipboard").exists())
        assert (work / "clipboard").read_text() == f"@{plan}#L3-4", (work / "clipboard").read_text()
        assert f"Copied @{plan}#L3-4" in m.screen(), "the status line says what was copied"
        m.press("right", "cmd+shift+c")
        wait_for("the cursor's line", lambda: (work / "clipboard").read_text() == f"@{plan}#L5")
        m.quit()
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
              "option+] found the change, the command bar replaced all; cmd+shift+C copied @file#L3-4, and the "
              "cursor's line with nothing selected")

        # two sessions to ask
        first = session("start", "--cwd", str(project), "--", "hello").split()[1]
        second = session("start", "--cwd", str(project), "--", "second").split()[1]
        wait_for("the sessions", lambda: session("list").count(" idle ") == 2)

        # 3. cmd+L: the column, and the box a question is written in
        m = editor(first, "first", "here")
        assert "→ first" in m.screen().splitlines()[-2], "the status line says whom questions go to"
        assert not m.column(), "no column before a session is asked"
        m.press("down", "down", "shift+down", "shift+down", "cmd+l")
        wait_for("the column, the cursor in its box", lambda: "Ask first · plan.md L3-4" in m.foot() and m.in_box())
        assert "enter sends" in m.right(), "the line over the box names its keys: " + m.right()
        m.type("make these")
        m.press("shift+enter")
        m.type("x")
        wait_for("a second line in the box", lambda: m.box() == ["make these", "x"])
        m.press("backspace", "backspace")
        wait_for("it taken out again", lambda: m.box() == ["make these"])
        m.press("esc")
        wait_for("esc to go back to the file", lambda: not m.in_box())
        assert m.box() == ["make these"], "what was written stays in the box"
        assert f"@{plan}" not in sent_to(socket, first), "nothing was sent"
        m.press("cmd+l")
        wait_for("the box again", lambda: m.in_box())
        m.type(" shorter")
        m.press("enter")
        wait_for("the cursor back in the file, the box empty", lambda: not m.in_box() and m.box() == [])
        m.press("cmd+down")
        m.type("typed while it works ")
        wait_for("the reply above the box", lambda: "Echo: make these shorter" in m.right())
        assert "You → first" in m.right() and "make these shorter" in m.right(), m.right()
        assert "⏺ first" in m.right(), "the reply starts under the session's name: " + m.right()
        asked, replied = m.drawn("\u00a0make these", "shorter"), m.drawn("Echo: make these", "shorter")
        assert asked and replied and asked != replied, \
            f"what you asked is drawn in another color than the reply: {asked!r} and {replied!r}"
        lit, plain = m.drawn("\u00a0make these", "shorter", "48"), m.drawn("Echo: make these", "shorter", "48")
        assert lit and plain and lit != plain, \
            f"and on a background of its own, highlighted: {lit!r} and the reply's {plain!r}"
        whom, name = m.drawn("You → first", "You"), m.drawn("⏺ first", "first")
        assert whom and name and len({asked, replied, whom, name}) == 4, \
            f"and so are the line that says whom you asked and the name the reply starts under: {whom!r}, {name!r}"
        wait_for("the question in the session", lambda: f"@{plan}#L3-4" in sent_to(socket, first))
        assert any("typed while it works" in line for line in m.left()), "typing went on meanwhile"
        m.press("cmd+enter")
        wait_for("the reply at the cursor", lambda: any(
            line.endswith("typed while it works Echo: make these shorter") for line in m.left()))
        m.press("cmd+z")
        wait_for("it taken back", lambda: not any("Echo" in line for line in m.left()))
        # cmd+O has y show the session; this test's micro is in no WezTerm pane, which y says.
        m.press("cmd+o")
        wait_for("cmd+O to ask y for the session's window",
                 lambda: "Couldn't open first: this isn't a WezTerm pane" in m.screen().splitlines()[-1])
        print("ok   cmd+L opened the column with the cursor in its box: shift+enter was a new line, esc went back "
              "to the file with the question kept, enter sent it about the file and lines 3-4; typing went on; "
              "the reply showed above the box, drawn apart from the question; cmd+enter put it at the cursor and "
              "cmd+Z took it back; cmd+O asked for the session's window")

        # 4. another session, picked from the list above the box
        m.press("cmd+shift+l")
        wait_for("the list of sessions", lambda: m.right().count("+ New session in project") == 2
                 and "idle" in m.right() and "listing sessions" not in m.right())
        assert "Pick a session" in m.foot() and m.in_box(), m.screen()
        assert "# Plan" in "\n".join(m.left()), "the file stays in view beside the list"
        rows = [line for line in m.right().splitlines() if "+ New session in project" in line]
        assert "Claude Code" in rows[0] and "Codex" in rows[1], "a row to start each agent's session: " + m.screen()
        assert first not in m.screen(), "the ids stay out of sight"
        assert sum("Claude Code" in line and "○ idle" in line and "project" in line
                   for line in m.right().splitlines()) == 2, \
            "each session's row says who runs it, what it does and where: " + m.screen()
        marked = [line for line in m.right().splitlines() if line.lstrip().startswith("▶")]
        assert len(marked) == 1 and "○ idle" in marked[0], \
            "the mark starts on the session questions go to: " + m.right()
        claude, codex_color = m.drawn("Claude Code  + New", "✳"), m.drawn("Codex", "◆")
        assert claude and codex_color and claude != codex_color, \
            f"Claude Code's rows and Codex's are in a color each: {claude!r} and {codex_color!r}"
        m.type("codex new")
        wait_for("the rows that have both words", lambda: "Claude Code" not in m.right()
                 and m.right().count("+ New session in project") == 1)
        for _ in "codex new":
            m.press("backspace")
        wait_for("every row again", lambda: m.right().count("+ New session in project") == 2 and "idle" in m.right())
        m.press("esc")
        wait_for("esc to leave the list for the replies", lambda: "Echo: make these shorter" in m.right()
                 and "Ask first" in m.foot())
        assert m.in_box(), "the cursor stays in the box, which is for a question again"
        m.press("cmd+shift+l")
        wait_for("the list again", lambda: "idle" in m.right() and "listing sessions" not in m.right())
        m.press("down", "up", "down", "enter")  # from the first session's row to the second's
        wait_for("the box, for a question to the second", lambda: "Ask " in m.foot() and "Ask first" not in m.foot())
        assert m.in_box(), "the cursor stays in the box"
        m.type("and you")
        m.press("enter")
        wait_for("its reply", lambda: "Echo: and you" in m.right())
        assert "Echo: make these shorter" in m.right(), "the first reply stays above"
        wait_for("the question in the second session", lambda: "and you" in sent_to(socket, second))
        print("ok   cmd+shift+L listed the sessions above the box, the file in view; typing filtered them, the "
              "arrows moved the mark, esc left the list; the second was asked too, and both replies show under "
              "their questions")

        # 5. a session the question starts
        m.press("cmd+shift+l")
        wait_for("the list of sessions", lambda: "listing sessions" not in m.right() and "idle" in m.right())
        m.type("new claude")
        wait_for("its row alone", lambda: m.right().count("+ New session in project") == 1
                 and "Codex" not in m.right())
        m.press("enter")
        wait_for("the box, for a new session", lambda: "Ask new · project" in m.foot())
        m.type("start with this")
        m.press("enter")
        wait_for("the new session's reply", lambda: "Ready." in m.right())
        # It is the editor's own: in a window of Marmota's tmux, and not listed.
        def windows() -> list[str]:
            out = subprocess.run(["tmux", "-L", socket, "list-windows", "-a", "-F", "#{window_name}"],
                                 capture_output=True, text=True).stdout.split()
            return sorted({name[3:] for name in out if name.startswith("cc-")})
        started = [sid for sid in windows() if sid not in (first, second)]
        assert len(started) == 1, windows()
        assert started[0] not in session("list"), "a temporary session isn't listed: " + session("list")
        m.press("cmd+l")
        wait_for("the box, for the session it started", lambda: "Ask new · project" in m.foot() and m.in_box())
        m.type("a follow-up")
        m.press("enter")
        wait_for("the follow-up's reply", lambda: "Echo: a follow-up" in m.right())
        wait_for("the follow-up in the new session", lambda: "a follow-up" in sent_to(socket, started[0]))
        m.quit()
        wait_for("the session to close with the editor", lambda: started[0] not in windows(), timeout=20)
        assert first in windows() and second in windows(), "the others go on"
        print("ok   the list's first row started a session with the question, and the next question went on "
              "with it; it wasn't listed, and closed with the editor")

        # 6. cmd+K: the session changes the lines in the file
        m = editor(first, "first", "here")
        m.press("down", "down", "shift+down", "shift+down", "cmd+k")
        wait_for("the box, for a change", lambda: "first changes plan.md L3-4" in m.foot() and m.in_box())
        m.type("in capitals")
        m.press("enter")
        wait_for("the change in the editor", lambda: m.left()[2:5] == ["LINE THREE", "LINE FOUR", "line five"])
        assert plan.read_text().splitlines()[2:4] == ["LINE THREE", "LINE FOUR"], "the file itself changed"
        assert "Put lines 3-4 in capitals." in m.right()
        assert "changed plan.md" in m.screen().splitlines()[-1]
        assert not m.in_box() and "Ask first" in m.foot(), "the cursor is in the file, the box for a question again"
        m.press("cmd+enter")
        time.sleep(0.5)
        assert "Put lines" not in "\n".join(m.left()), "what it said of its change isn't a reply to put in"
        m.press("cmd+z")
        wait_for("the change taken back", lambda: m.left()[2:4] == ["line three", "line four"])
        print("ok   cmd+K had the box take what to change in lines 3-4; the session changed them in the file, the "
              "editor showed them and cmd+Z took the change back")

        # 7. the column put away and back, and in another tab
        m.press("cmd+l")
        wait_for("the box", lambda: m.in_box())
        m.type("a draft")
        m.press("cmd+l")
        wait_for("cmd+L from the box to go back to the file", lambda: not m.in_box())
        m.press("cmd+j")
        wait_for("cmd+J to put the column away", lambda: not m.column())
        m.press("cmd+j")
        wait_for("the column again, with what the box held", lambda: m.box() == ["a draft"])
        assert "Put lines 3-4 in capitals." in m.right() and not m.in_box(), m.screen()
        m.press("cmd+l", "cmd+w")
        wait_for("cmd+W in the box to put the column away", lambda: not m.column())
        assert "# Plan" in m.screen(), "the file stays open"
        m.press("opt+x")
        m.type("tab notes.md")
        m.press("enter")
        wait_for("another tab", lambda: "notes.md" in m.screen().splitlines()[0])
        m.press("cmd+l")
        wait_for("the column in this tab, with what was asked", lambda: "Put lines 3-4 in capitals." in m.right()
                 and "Ask first · notes.md" in m.foot() and m.box() == ["a draft"])
        m.press("esc", "opt+,")
        wait_for("the first tab, without the column", lambda: "# Plan" in m.screen() and not m.column())
        print("ok   cmd+J put the column away and showed it again with what the box held; cmd+L from the box "
              "went back to the file; cmd+W in the box put the column away; in another tab it came along")

        # 8. A click on a question in the replies, or enter on it, selects
        # the lines it was about in the file again, where they are now. The
        # question's line names no lines.
        m.quit()
        m = editor(first, "first", "here")
        m.press("down", "down", "shift+down", "shift+down", "cmd+l")
        wait_for("the box", lambda: m.in_box())
        m.type("where are these")
        m.press("enter")
        wait_for("the reply", lambda: "Echo: where are these" in m.right())
        whom = [line for line in m.right().splitlines() if "You → first" in line]
        assert whom and whom[0].strip() == "You → first", "the question's line names only whom: " + m.right()

        def click(text: str) -> None:
            """A click (press and release) on the first line of the screen that has text, on its first letter."""
            for y, line in enumerate(m.screen().splitlines()):
                if text in line:
                    x = len(line.split(text, 1)[0]) + 1
                    m.mouse(0, x, y + 1)
                    m.mouse(0, x, y + 1, release=True)
                    return
            raise AssertionError(f"{text!r} isn't on the screen:\n{m.screen()}")

        def back() -> None:
            """The keyboard to the replies, enter, and it back in the file."""
            m.press("opt+w")
            wait_for("the replies to have the keyboard", lambda: "enter selects" in m.right())
            m.press("enter")
            wait_for("the keyboard back in the file", lambda: "enter sends" in m.right())

        def shown() -> list[str]:
            """The file's lines on the screen, without the line numbers and the marks of changed lines
            left of them."""
            out = []
            for line in m.screen().splitlines()[:-2]:
                line = line.split("|")[0]
                out.append(re.sub(r"^\S?\s*\d+(?: |$)", "", line).rstrip())
            return out

        def selected(rows: slice, lines: list[str], before: list[str]) -> None:
            """What is selected is those rows: typing replaces them, and cmd+Z takes it back."""
            time.sleep(0.6)  # micro undoes what was done within half a second as one step
            m.type("X")
            wait_for("X in place of the selected lines", lambda: shown()[rows] == lines)
            for _ in range(3):
                time.sleep(0.4)
                if shown()[:len(before)] == before:
                    break
                m.press("cmd+z")
            wait_for("the lines back", lambda: shown()[:len(before)] == before)

        m.press("cmd+g")
        m.type("t")
        back()
        selected(slice(2, 4), ["X", "line five"], ["# Plan", "", "line three", "line four", "line five"])
        # a click on what was asked does what enter does
        m.press("cmd+g")
        m.type("t")
        click("where are these")
        wait_for("the keyboard back in the file", lambda: "enter sends" in m.right())
        selected(slice(2, 4), ["X", "line five"], ["# Plan", "", "line three", "line four", "line five"])
        m.press("cmd+g")
        m.type("t")
        m.type("new one")
        m.press("enter")
        m.type("new two")
        m.press("enter")
        moved = ["new one", "new two", "# Plan", "", "line three", "line four", "line five"]
        click("You → first")
        wait_for("the keyboard back in the file", lambda: "enter sends" in m.right())
        selected(slice(4, 6), ["X", "line five"], moved)
        m.press("cmd+g")
        m.type("5")
        m.press("enter")
        m.type("Z")
        back()
        wait_for("it to say they changed", lambda: "these lines changed since you asked" in m.screen())
        m.press("esc")

        # cmd+K: the lines the session's change spans
        m.quit()
        m = editor(first, "first", "here")
        m.press("down", "down", "shift+down", "shift+down", "cmd+k")
        wait_for("the box, for a change", lambda: "first changes plan.md L3-4" in m.foot() and m.in_box())
        m.type("in capitals and add a line")
        m.press("enter")
        changed = ["# Plan", "", "LINE THREE", "LINE FOUR", "ADDED", "line five"]
        wait_for("the change in the editor", lambda: shown()[:6] == changed)
        wait_for("the change done", lambda: "changed plan.md" in m.screen().splitlines()[-1])
        m.press("cmd+g")
        m.type("t")
        back()
        selected(slice(2, 4), ["X", "line five"], changed)
        # the file closed: enter opens it again, in a tab of its own
        m.press("cmd+w")
        if "Save changes" in m.screen():
            m.type("n")
        wait_for("the file's pane closed", lambda: "LINE THREE" not in m.screen())
        # the column is all the tab holds now, without the line between panes
        if "enter selects" not in m.screen():
            m.press("opt+w")
        wait_for("the replies to have the keyboard", lambda: "enter selects" in m.screen())
        m.press("enter")
        wait_for("the file in a new tab", lambda: "[plan.md]" in m.screen().splitlines()[0]
                 and shown()[1:7] == changed)
        selected(slice(3, 5), ["X", "line five"], [m.screen().splitlines()[0].rstrip(), *changed])
        print("ok   a click or enter on a question in the replies selected its lines in the file, where they "
              "had moved to, and said when they had changed since; for cmd+K's, the lines its change spans")

        # 9. The editor put away. A micro that Marmota's e didn't open, as
        # this one, has nothing to go back to and says so.
        m.press("cmd+m")
        wait_for("micro to say it stays", lambda: "Nothing to go back to here" in m.screen())
        m.quit()
        for name in list(project.iterdir()):
            if name != plan:
                name.unlink()  # the file panel's cursor is on plan.md
        plan.write_text(PLAN)
        shutil.rmtree(config / "backups", ignore_errors=True)
        m = Marmota(server, project, env, config, y)
        m.type("e")
        wait_for("micro in Marmota's place", lambda: "# Plan" in m.screen() and m.runs() == "micro")
        m.type("DRAFT ")
        wait_for("the typing", lambda: "DRAFT # Plan" in m.screen())
        m.press("cmd+m")
        away = "✎ micro · 1 file · b back"
        wait_for("Marmota with the editor put away", lambda: away in m.screen())
        assert m.runs() != "micro", m.runs()
        assert plan.read_text() == PLAN, "nothing was saved"
        print("ok   cmd+M put micro away: Marmota is back, and says an editor is put away")

        m.type("e")  # e stays what it was: another editor
        wait_for("a second micro", lambda: "# Plan" in m.screen() and "DRAFT" not in m.screen())
        m.press("cmd+m")
        wait_for("it to say one is put away", lambda: "An editor is put away already" in m.screen())
        m.press("cmd+w")
        wait_for("Marmota again", lambda: away in m.screen())
        print("ok   e opened another micro meanwhile, which said one is put away already")

        m.type("b")
        wait_for("the first micro back", lambda: "DRAFT # Plan" in m.screen() and m.runs() == "micro")
        print("ok   b brought it back with what was typed")

        m.press("cmd+m")
        wait_for("Marmota", lambda: away in m.screen())
        m.type("q")
        wait_for("micro back instead of quitting", lambda: "Marmota didn't quit" in m.screen())
        assert "DRAFT # Plan" in m.screen(), m.screen()
        m.press("cmd+s")
        wait_for("the file saved", lambda: plan.read_text() == "DRAFT " + PLAN)
        m.press("cmd+m")
        wait_for("Marmota", lambda: away in m.screen())
        m.type("q")
        wait_for("Marmota to quit", lambda: "MARMOTA-ENDED" in m.screen())
        left = subprocess.run(["pgrep", "-f", f"micro {plan}"], capture_output=True, text=True).stdout.split()
        assert not left, f"micro still runs: {left}"
        print("ok   q brought micro back while the file wasn't saved, and ended it and quit once it was")
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
