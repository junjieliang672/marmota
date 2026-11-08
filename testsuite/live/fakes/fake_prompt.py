#!/usr/bin/env python3
"""The prompt box of the fake claude, drawn and driven as Claude Code's is.

superfile reads a session's prompt box before it types into it, so the fake
session draws one: a rule, "❯" and a no-break space before what is typed (a
dim placeholder when nothing is), a rule. It takes typing, a paste (a long one
shows as "[Pasted text #1 +N lines]"), ctrl+s (stash, or bring the stash back
into an empty box), ctrl+_ (undo the last paste) and enter, which sends what is
typed, shows it above the box, and brings back a stashed prompt.

    fake_prompt.py TITLE
"""
from __future__ import annotations

import os
import shutil
import signal
import sys
import termios
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
            self.text, self.shown, self.undo = "", "", []
            if self.stash is not None:
                (self.text, self.shown), self.stash = self.stash, None
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
