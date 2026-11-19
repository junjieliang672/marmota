#!/usr/bin/env python3
"""The prompt box of the fake claude, drawn and driven as Claude Code's is.

superfile reads a session's prompt box before it types into it, so the fake
session draws one: a rule, "❯" and a no-break space before what is typed (a
dim placeholder when nothing is), a rule. It takes typing, a paste (a long one
shows as "[Pasted text #1 +N lines]"), ctrl+s (stash, or bring the stash back
into an empty box), ctrl+_ (undo the last paste) and enter, which sends what is
typed, shows it above the box, and brings back a stashed prompt. A line feed
typed starts a new line in the box, as in Claude Code. What is sent is
written to the session's transcript as a turn, with a reply and its end.

    fake_prompt.py TITLE
"""
from __future__ import annotations

import json
import os
import re
import shutil
import signal
import sys
import termios
import time
import tty

PASTE_START, PASTE_END = b"\x1b[200~", b"\x1b[201~"
STASH, UNDO, ENTER, BACKSPACE = b"\x13", b"\x1f", b"\r", b"\x7f"


class Prompt:
    def __init__(self, title: str) -> None:
        self.title = title
        self.text = ""
        self.shown = ""  # what the box shows: a long paste as its summary
        self.stash: tuple[str, str] | None = None
        self.undo: list[tuple[str, str]] = []
        self.pastes = 0
        self.sent: list[str] = []

    def paste(self, text: str) -> None:
        self.undo.append((self.text, self.shown))
        self.text += text
        if text.count("\n") >= 2:
            self.pastes += 1
            self.shown += f"[Pasted text #{self.pastes} +{text.count(chr(10))} lines]"
        else:
            self.shown += text

    def record(self, text: str) -> None:
        """Writes the turn a sent message makes to the session's transcript,
        as Claude Code does: the message, a reply ("Echo: " and its first
        line), and the turn's end."""
        reg = os.environ.get("FAKE_CLAUDE_REG", "")
        try:
            transcript = open(os.path.join(reg, self.title + ".transcript")).read().strip()
        except OSError:
            return
        now = time.time()

        def stamp(after_ms: int) -> str:
            t = now + after_ms / 1000
            return time.strftime("%Y-%m-%dT%H:%M:%S", time.gmtime(t)) + ".%03dZ" % int(t % 1 * 1000)

        reply = "Echo: " + (text.strip().splitlines() or [""])[0]
        # Asked to change a file in place, as Marmota's plugin for micro
        # asks: the lines named are put in capitals, the whole file with none;
        # asked to add a line too, "ADDED" goes under them.
        edit = re.search(r'editing the file in place: @"?([^"#\s]+)"?(?:#L(\d+)(?:-(\d+))?)?', text)
        if edit:
            try:
                lines = open(edit.group(1)).read().split("\n")
                first = int(edit.group(2) or 1)
                last = int(edit.group(3) or edit.group(2) or len(lines))
                for i in range(first - 1, min(last, len(lines))):
                    lines[i] = lines[i].upper()
                reply = "Put lines %d-%d in capitals." % (first, last)
                if "add a line" in text:
                    lines.insert(min(last, len(lines)), "ADDED")
                    reply = "Put lines %d-%d in capitals and added a line." % (first, last)
                open(edit.group(1), "w").write("\n".join(lines))
            except OSError as err:
                reply = "Couldn't edit: %s" % err
        with open(transcript, "a") as f:
            f.write(json.dumps({"type": "user", "message": {"role": "user", "content": text},
                                "timestamp": stamp(0), "sessionId": self.title}) + "\n")
            f.write(json.dumps({"type": "assistant", "timestamp": stamp(1), "message": {
                "role": "assistant", "content": [{"type": "text", "text": reply}]}}) + "\n")
            f.write(json.dumps({"type": "system", "subtype": "turn_duration", "durationMs": 2,
                                "timestamp": stamp(2)}) + "\n")

    def key(self, key: bytes) -> None:
        if key == STASH:
            if self.text.strip() == "" and self.stash is not None:
                (self.text, self.shown), self.stash = self.stash, None
            elif self.text.strip() != "":
                self.stash, self.text, self.shown = (self.text, self.shown), "", ""
        elif key == UNDO and self.undo:
            self.text, self.shown = self.undo.pop()
        elif key == ENTER and self.text:
            self.sent.append(self.text)
            self.record(self.text)
            self.text, self.shown, self.undo = "", "", []
            if self.stash is not None:
                (self.text, self.shown), self.stash = self.stash, None
        elif key == b"\n":
            # A line feed typed, as shift+enter sends it: a new line in the box.
            self.text, self.shown = self.text + "\n", self.shown + "\n"
        elif key == BACKSPACE:
            self.text, self.shown = self.text[:-1], self.shown[:-1]
        elif key.isascii() and key.decode().isprintable():
            self.paste(key.decode())

    def draw(self) -> None:
        cols, rows = shutil.get_terminal_size((80, 24))
        rule = "─" * max(8, cols)
        # No auto-wrap: rows wrap within the box instead.
        out = ["\x1b[?7l\x1b[H\x1b[2J", f"fake claude {self.title} ready\r\n"]
        for message in self.sent:
            for line in message.splitlines() or [""]:
                out.append(f"\x1b[48;5;254m❯\x1b[0m {line}\r\n")
        # Rows wrap within the box, indented under the text, as Claude
        # Code's do; the placeholder too, dim.
        room = max(1, cols - 2)

        def rows_of(text: str) -> list[str]:
            return [text[i:i + room] for i in range(0, max(1, len(text)), room)]

        style = "" if self.text else "\x1b[2m"
        source = self.shown if self.text else 'Try "fix lint errors"'
        box = [row for line in source.split("\n") for row in rows_of(line)]
        # The box ends just above the last three rows; "› stashed" sits on
        # the row above it.
        rule_row = max(len(self.sent) + 2, rows - 3 - len(box))
        if self.stash is not None:
            out.append(f"\x1b[{rule_row - 1};{max(1, cols - 12)}H\x1b[2m › stashed\x1b[0m")
        out.append(f"\x1b[{rule_row};1H{rule[:cols]}\r\n")
        out.append("❯\N{NO-BREAK SPACE}" + style + box[0] + "\x1b[0m\r\n")
        out.extend("  " + style + row + "\x1b[0m\r\n" for row in box[1:])
        out.append(rule[:cols] + "\r\n" + "  ⏵⏵ bypass permissions on"[:cols])
        sys.stdout.write("".join(out))
        sys.stdout.flush()


def main() -> None:
    prompt = Prompt(sys.argv[1] if len(sys.argv) > 1 else "")
    fd = sys.stdin.fileno()
    saved = termios.tcgetattr(fd)
    tty.setraw(fd)
    sys.stdout.write("\x1b[?2004h")  # bracketed paste, as Claude Code asks for
    signal.signal(signal.SIGWINCH, lambda *_: prompt.draw())
    try:
        prompt.draw()
        pending = b""
        while True:
            chunk = os.read(fd, 4096)
            if not chunk:
                return
            pending += chunk
            while pending:
                if pending.startswith(PASTE_START):
                    end = pending.find(PASTE_END)
                    if end < 0:
                        break  # the rest of the paste is on its way
                    prompt.paste(pending[len(PASTE_START):end].decode(errors="replace").replace("\r", "\n"))
                    pending = pending[end + len(PASTE_END):]
                elif pending.startswith(b"\x1b"):
                    pending = pending[1:]  # other escape sequences mean nothing here
                else:
                    prompt.key(pending[:1])
                    pending = pending[1:]
            prompt.draw()
    finally:
        termios.tcsetattr(fd, termios.TCSADRAIN, saved)


if __name__ == "__main__":
    main()
