"""The fake terminal that sits between the pty and pyte.

pyte is a good VT100 screen, but it does not answer terminal queries, and it
mis-parses a few modern sequences (it ignores the ``>`` in ``CSI > 4;1 m`` and
would turn that into bold+underline, and it draws the payload of APC/DCS
strings such as Kitty graphics as text). OutputFilter tokenizes the program's
output incrementally, so sequences split across reads are handled, and turns it
into an ordered list of events:

* ``text``: bytes that are safe to feed to pyte.
* ``query``: a terminal query that needs an answer, in stream order. Cursor
  position reports need the screen state at that exact point, so the session
  feeds pyte everything before a query before answering it.
* ``title``, ``clipboard`` (OSC 52), ``graphics`` (Kitty/sixel), ``mode``.
"""
from __future__ import annotations

import base64
import binascii
import re
from dataclasses import dataclass, field
from typing import Dict, List, Optional, Set, Tuple

ESC: str = "\x1b"
ST: str = ESC + "\\"
BEL: str = "\x07"

# Catppuccin Mocha, matching superfile's default theme, so areas that use the
# terminal default colors blend in. OSC 10/11 queries are answered with these.
_DEFAULT_ANSI: Tuple[str, ...] = (
    "#45475a", "#f38ba8", "#a6e3a1", "#f9e2af",
    "#89b4fa", "#f5c2e7", "#94e2d5", "#bac2de",
    "#585b70", "#f38ba8", "#a6e3a1", "#f9e2af",
    "#89b4fa", "#f5c2e7", "#94e2d5", "#a6adc8",
)

# pyte color names (including its "bfightmagenta" typo) -> ANSI index.
_PYTE_COLOR_INDEX: Dict[str, int] = {
    "black": 0, "red": 1, "green": 2, "brown": 3, "yellow": 3,
    "blue": 4, "magenta": 5, "cyan": 6, "white": 7,
    "brightblack": 8, "brightred": 9, "brightgreen": 10, "brightbrown": 11,
    "brightyellow": 11, "brightblue": 12, "brightmagenta": 13,
    "bfightmagenta": 13, "brightcyan": 14, "brightwhite": 15,
}

_HEX6 = re.compile(r"^[0-9a-fA-F]{6}$")


def hex_to_rgb(color: str) -> Tuple[int, int, int]:
    """Convert "#rrggbb" (or "rrggbb") into an (r, g, b) tuple."""
    value = color.lstrip("#")
    return int(value[0:2], 16), int(value[2:4], 16), int(value[4:6], 16)


@dataclass(frozen=True)
class Palette:
    """Default colors of the fake terminal, as "#rrggbb" strings."""

    foreground: str = "#cdd6f4"
    background: str = "#1e1e2e"
    cursor: str = "#f5e0dc"
    ansi: Tuple[str, ...] = _DEFAULT_ANSI

    def resolve(self, value: str, *, foreground: bool) -> str:
        """Map a pyte color ("default", "red", "ff8800") to "#rrggbb"."""
        if value == "default" or not value:
            return self.foreground if foreground else self.background
        if _HEX6.match(value):
            return "#" + value.lower()
        index = _PYTE_COLOR_INDEX.get(value)
        if index is not None:
            return self.ansi[index]
        return self.foreground if foreground else self.background

    def osc_rgb(self, color: str) -> str:
        """Format a color the way xterm answers OSC 10/11 queries."""
        r, g, b = hex_to_rgb(color)
        return f"rgb:{r:02x}{r:02x}/{g:02x}{g:02x}/{b:02x}{b:02x}"


@dataclass(frozen=True)
class Event:
    """One item of filtered terminal output, see the module docstring."""

    kind: str
    data: str = ""
    arg: str = ""
    term: str = ""


_GROUND, _ESC, _ESC_INTER, _CSI, _STR, _STR_ESC = range(6)
_SPECIAL = re.compile("[\x1b\x90\x98\x9b\x9c\x9d\x9e\x9f]")
_STR_END = re.compile("[\x07\x1b\x9c]")
_STR_KINDS: Dict[str, str] = {"]": "osc", "P": "dcs", "_": "apc", "^": "pm", "X": "sos"}
_C1_STR_KINDS: Dict[str, str] = {
    "\x9d": "osc", "\x90": "dcs", "\x9f": "apc", "\x9e": "pm", "\x98": "sos"}
_CSI_RE = re.compile(r"^\x1b\[([<=>?]?)([0-9;:]*)([ -/]*)([@-~])$")
_MAX_CSI_LEN = 256
_MAX_STRING_CAPTURE = 1 << 20


def sanitize_sgr(params: str) -> Optional[str]:
    """Rewrite SGR parameters into the subset pyte understands.

    Colon sub-parameters (``38:2::r:g:b``, ``4:3``) are converted to their
    semicolon forms, and underline colors (58/59) are removed because pyte
    would read their arguments as attributes. Returns None when nothing
    is left, so the caller drops the sequence instead of sending ``CSI m``
    (which would reset all attributes).
    """
    if ":" not in params and "58" not in params and "59" not in params:
        return params
    parts = params.split(";")
    out: List[str] = []
    i = 0
    while i < len(parts):
        part = parts[i]
        if ":" in part:
            sub = part.split(":")
            head = sub[0]
            if head in ("38", "48"):
                if len(sub) >= 3 and sub[1] == "5":
                    out += [head, "5", sub[2] or "0"]
                elif len(sub) >= 5 and sub[1] == "2":
                    out += [head, "2"] + [x or "0" for x in sub[-3:]]
            elif head == "4":
                style = sub[1] if len(sub) > 1 else "1"
                out.append("24" if style == "0" else "4")
            i += 1
            continue
        if part in ("38", "48", "58"):
            take = 1
            if i + 1 < len(parts) and parts[i + 1] == "5":
                take = 3
            elif i + 1 < len(parts) and parts[i + 1] == "2":
                take = 5
            if part != "58":
                out += parts[i:i + take]
            i += take
            continue
        if part != "59":
            out.append(part)
        i += 1
    if not out:
        return None
    return ";".join(out)


class OutputFilter:
    """Incremental tokenizer for program output, see the module docstring."""

    def __init__(self) -> None:
        self._state: int = _GROUND
        self._seq: List[str] = []
        self._str_kind: str = ""
        self._str_parts: List[str] = []
        self._str_len: int = 0

    def feed(self, data: str) -> List[Event]:
        events: List[Event] = []
        text: List[str] = []
        i: int = 0
        n: int = len(data)
        while i < n:
            state = self._state
            if state == _GROUND:
                match = _SPECIAL.search(data, i)
                if match is None:
                    text.append(data[i:])
                    break
                j = match.start()
                if j > i:
                    text.append(data[i:j])
                char = data[j]
                i = j + 1
                if char == ESC:
                    self._state = _ESC
                    self._seq = [char]
                elif char == "\x9b":
                    self._state = _CSI
                    self._seq = [ESC, "["]
                elif char in _C1_STR_KINDS:
                    self._begin_string(_C1_STR_KINDS[char])
                # A stray ST (\x9c) is dropped.
            elif state == _ESC:
                char = data[i]
                i += 1
                self._on_escape_char(char, text)
            elif state == _ESC_INTER:
                char = data[i]
                i += 1
                self._seq.append(char)
                if not " " <= char <= "/":
                    text.append("".join(self._seq))
                    self._state = _GROUND
                elif len(self._seq) > 8:
                    self._state = _GROUND
            elif state == _CSI:
                char = data[i]
                i += 1
                self._on_csi_char(char, text, events)
            elif state == _STR:
                match = _STR_END.search(data, i)
                if match is None:
                    self._capture(data[i:])
                    break
                j = match.start()
                self._capture(data[i:j])
                char = data[j]
                i = j + 1
                if char == ESC:
                    self._state = _STR_ESC
                else:
                    self._finish_string(BEL if char == BEL else ST, text, events)
            else:  # _STR_ESC
                if data[i] == "\\":
                    i += 1
                    self._finish_string(ST, text, events)
                else:
                    # ESC inside a string aborts it and starts a new sequence.
                    self._state = _ESC
                    self._seq = [ESC]
        _flush(text, events)
        return events

    def _on_escape_char(self, char: str, text: List[str]) -> None:
        if char == "[":
            self._state = _CSI
            self._seq.append(char)
        elif char in _STR_KINDS:
            self._begin_string(_STR_KINDS[char])
        elif char == "\\":
            self._state = _GROUND
        elif char == ESC:
            self._seq = [ESC]
        elif " " <= char <= "/":
            self._seq.append(char)
            self._state = _ESC_INTER
        else:
            self._seq.append(char)
            text.append("".join(self._seq))
            self._state = _GROUND

    def _on_csi_char(self, char: str, text: List[str], events: List[Event]) -> None:
        code = ord(char)
        if 0x40 <= code <= 0x7E:
            self._seq.append(char)
            self._state = _GROUND
            self._finish_csi("".join(self._seq), text, events)
        elif 0x20 <= code <= 0x3F:
            self._seq.append(char)
            if len(self._seq) > _MAX_CSI_LEN:
                self._state = _GROUND
        elif char == ESC:
            self._seq = [ESC]
            self._state = _ESC
        elif char in "\x18\x1a":
            self._state = _GROUND
        elif code < 0x20:
            # C0 controls execute immediately, even inside a CSI sequence.
            text.append(char)
        else:
            self._state = _GROUND

    def _begin_string(self, kind: str) -> None:
        self._state = _STR
        self._str_kind = kind
        self._str_parts = []
        self._str_len = 0

    def _capture(self, chunk: str) -> None:
        if chunk and self._str_len < _MAX_STRING_CAPTURE:
            self._str_parts.append(chunk)
            self._str_len += len(chunk)

    def _finish_string(self, term: str, text: List[str], events: List[Event]) -> None:
        self._state = _GROUND
        payload = "".join(self._str_parts)
        self._str_parts = []
        kind = self._str_kind
        event: Optional[Event] = None
        if kind == "osc":
            event = _osc_event(payload, term)
        elif kind == "apc" and payload.startswith("G"):
            event = Event("graphics", "kitty", payload[:200])
        elif kind == "dcs" and re.match(r"^[0-9;]*q", payload):
            event = Event("graphics", "sixel", payload[:200])
        if event is not None:
            _flush(text, events)
            events.append(event)

    def _finish_csi(self, seq: str, text: List[str], events: List[Event]) -> None:
        match = _CSI_RE.match(seq)
        if match is None:
            return
        prefix, params, inter, final = match.groups()
        query = _csi_query(prefix, params, inter, final)
        if query is not None:
            _flush(text, events)
            events.append(query)
            return
        if prefix in ("<", "=", ">") or inter:
            return  # pyte would misinterpret these
        if final == "m":
            if prefix:
                return
            cleaned = sanitize_sgr(params)
            if cleaned is not None:
                text.append(f"{ESC}[{cleaned}m")
            return
        if ":" in params:
            return
        if prefix == "?" and final in "hl":
            _flush(text, events)
            events.append(Event("mode", final, params))
        if prefix == "?" and final in "nu":
            return  # unanswered queries (kitty keyboard, color scheme)
        text.append(seq)


def _flush(text: List[str], events: List[Event]) -> None:
    if text:
        events.append(Event("text", "".join(text)))
        text.clear()


def _csi_query(prefix: str, params: str, inter: str, final: str) -> Optional[Event]:
    if final == "n" and not inter:
        if prefix == "" and params == "6":
            return Event("query", "cpr")
        if prefix == "" and params == "5":
            return Event("query", "status")
        if prefix == "?" and params == "6":
            return Event("query", "xcpr")
        return None
    if final == "c" and not inter and params in ("", "0"):
        if prefix == "":
            return Event("query", "da1")
        if prefix == ">":
            return Event("query", "da2")
        return None
    if final == "t" and prefix == "" and not inter and params in ("14", "16", "18"):
        return Event("query", "winops", params)
    if final == "q" and prefix == ">" and params in ("", "0"):
        return Event("query", "xtversion")
    if final == "p" and inter == "$" and prefix in ("", "?"):
        return Event("query", "decrqm", params, prefix)
    return None


def _osc_event(payload: str, term: str) -> Optional[Event]:
    code, _, rest = payload.partition(";")
    if code in ("0", "2"):
        return Event("title", rest)
    if code in ("10", "11", "12") and rest == "?":
        return Event("query", "osc_color", code, term)
    if code == "4":
        fields = rest.split(";")
        indexes = [fields[k] for k in range(0, len(fields) - 1, 2) if fields[k + 1] == "?"]
        if indexes:
            return Event("query", "osc_palette", ";".join(indexes), term)
        return None
    if code == "52":
        _, _, encoded = rest.partition(";")
        if encoded == "?":
            return None
        try:
            decoded = base64.b64decode(encoded, validate=False).decode("utf-8", "replace")
        except (binascii.Error, ValueError):
            decoded = ""
        return Event("clipboard", decoded)
    return None


# Private modes we report through DECRQM. Others are answered "not recognized".
_KNOWN_PRIVATE_MODES: Set[int] = {
    1, 6, 7, 25, 47, 1000, 1002, 1003, 1004, 1005, 1006, 1015, 1047, 1048, 1049, 2004}


@dataclass
class TerminalState:
    """What the fake terminal reports about itself when queried."""

    cols: int
    rows: int
    cell_width: int
    cell_height: int
    palette: Palette = field(default_factory=Palette)
    private_modes: Set[int] = field(default_factory=set)

    def apply_mode(self, final: str, params: str) -> None:
        for raw in params.split(";"):
            if raw.isdigit():
                if final == "h":
                    self.private_modes.add(int(raw))
                else:
                    self.private_modes.discard(int(raw))

    def answer(self, event: Event, cursor_row: int, cursor_col: int) -> str:
        """Build the reply for a ``query`` event. cursor_* are 0-based."""
        kind = event.data
        if kind == "cpr":
            return f"{ESC}[{cursor_row + 1};{min(cursor_col, self.cols - 1) + 1}R"
        if kind == "xcpr":
            return f"{ESC}[?{cursor_row + 1};{min(cursor_col, self.cols - 1) + 1};1R"
        if kind == "status":
            return f"{ESC}[0n"
        if kind == "da1":
            return f"{ESC}[?62;22c"
        if kind == "da2":
            return f"{ESC}[>1;10;0c"
        if kind == "xtversion":
            return f"{ESC}P>|spf-tui-harness{ST}"
        if kind == "winops":
            if event.arg == "14":
                return f"{ESC}[4;{self.rows * self.cell_height};{self.cols * self.cell_width}t"
            if event.arg == "16":
                return f"{ESC}[6;{self.cell_height};{self.cell_width}t"
            return f"{ESC}[8;{self.rows};{self.cols}t"
        if kind == "osc_color":
            color = {"10": self.palette.foreground, "11": self.palette.background,
                     "12": self.palette.cursor}[event.arg]
            return f"{ESC}]{event.arg};{self.palette.osc_rgb(color)}{event.term}"
        if kind == "osc_palette":
            replies = []
            for raw in event.arg.split(";"):
                if raw.isdigit() and int(raw) < len(self.palette.ansi):
                    rgb = self.palette.osc_rgb(self.palette.ansi[int(raw)])
                    replies.append(f"{ESC}]4;{raw};{rgb}{event.term}")
            return "".join(replies)
        if kind == "decrqm":
            return self._answer_decrqm(event.arg, event.term == "?")
        return ""

    def _answer_decrqm(self, params: str, private: bool) -> str:
        mode = int(params) if params.isdigit() else 0
        if not private:
            return f"{ESC}[{mode};0$y"
        if mode not in _KNOWN_PRIVATE_MODES:
            return f"{ESC}[?{mode};0$y"
        value = 1 if mode in self.private_modes else 2
        return f"{ESC}[?{mode};{value}$y"
