"""Translate human readable key names into the bytes a terminal would send.

Only the legacy xterm encoding is produced, which is what Bubble Tea v1 parses.
Key specs are case-insensitive for names and modifiers, while a single
character is taken literally (``"J"`` is shift+j, ``"j"`` is j).

Examples::

    encode_key("enter")       -> b"\\r"
    encode_key("ctrl+k")      -> b"\\x0b"
    encode_key("alt+x")       -> b"\\x1bx"
    encode_key("shift+tab")   -> b"\\x1b[Z"
    encode_key("alt+shift+right") -> b"\\x1b[1;4C"
"""
from __future__ import annotations

import shlex
from typing import Dict, List, Optional, Tuple

ESC: str = "\x1b"

# Keys that are sent as a fixed sequence and cannot take modifiers in a
# meaningful way (other than alt, which prefixes ESC).
_PLAIN_KEYS: Dict[str, str] = {
    "enter": "\r",
    "return": "\r",
    "tab": "\t",
    "esc": ESC,
    "escape": ESC,
    "space": " ",
    "backspace": "\x7f",
    "bs": "\x7f",
}

# CSI based keys: name -> (prefix, final). Modified form is CSI 1;<mod><final>
# for letter finals, and CSI <num>;<mod>~ for tilde keys.
_CURSOR_KEYS: Dict[str, str] = {
    "up": "A",
    "down": "B",
    "right": "C",
    "left": "D",
    "home": "H",
    "end": "F",
}

_TILDE_KEYS: Dict[str, int] = {
    "insert": 2,
    "ins": 2,
    "delete": 3,
    "del": 3,
    "pgup": 5,
    "pageup": 5,
    "pgdown": 6,
    "pagedown": 6,
    "pgdn": 6,
    "f5": 15,
    "f6": 17,
    "f7": 18,
    "f8": 19,
    "f9": 20,
    "f10": 21,
    "f11": 23,
    "f12": 24,
}

_SS3_KEYS: Dict[str, str] = {
    "f1": "P",
    "f2": "Q",
    "f3": "R",
    "f4": "S",
}

_MODIFIER_ALIASES: Dict[str, str] = {
    "ctrl": "ctrl",
    "control": "ctrl",
    "alt": "alt",
    "meta": "alt",
    "opt": "alt",
    "option": "alt",
    "shift": "shift",
}

# Control characters reachable with ctrl+<punctuation>.
_CTRL_PUNCT: Dict[str, str] = {
    "@": "\x00",
    " ": "\x00",
    "space": "\x00",
    "[": "\x1b",
    "\\": "\x1c",
    "]": "\x1d",
    "^": "\x1e",
    "_": "\x1f",
    "?": "\x7f",
}

KNOWN_KEY_NAMES: Tuple[str, ...] = tuple(sorted(
    set(_PLAIN_KEYS) | set(_CURSOR_KEYS) | set(_TILDE_KEYS) | set(_SS3_KEYS)))


class KeySpecError(ValueError):
    """Raised for key specs that look like keys but cannot be encoded."""


def _xterm_modifier_param(shift: bool, alt: bool, ctrl: bool) -> int:
    return 1 + (1 if shift else 0) + (2 if alt else 0) + (4 if ctrl else 0)


def _split_modifiers(spec: str) -> Tuple[List[str], str]:
    """Split "ctrl+alt+x" into (["ctrl", "alt"], "x").

    A trailing "+" is the key itself, so "ctrl++" is ctrl and "+".
    """
    if len(spec) <= 1 or "+" not in spec[:-1]:
        return [], spec
    head, _, key = spec[:-1].rpartition("+")
    key = key + spec[-1]
    if not head:
        return [], spec
    mods: List[str] = []
    for raw in head.split("+"):
        mod = _MODIFIER_ALIASES.get(raw.lower())
        if mod is None:
            raise KeySpecError(f"unknown modifier {raw!r} in key spec {spec!r}")
        mods.append(mod)
    return mods, key


def encode_key(spec: str) -> Optional[bytes]:
    """Encode a single key spec, or return None if spec is not a key.

    Raises KeySpecError when spec has a modifier prefix but cannot be encoded
    (for example ``ctrl+shift+m``, which legacy terminals cannot express).
    """
    if not spec:
        return None
    if len(spec) == 1:
        return spec.encode("utf-8")

    if "+" in spec[:-1]:
        first = spec.split("+", 1)[0].lower()
        if first not in _MODIFIER_ALIASES:
            return None
    mods, key = _split_modifiers(spec)
    ctrl = "ctrl" in mods
    alt = "alt" in mods
    shift = "shift" in mods
    lower = key.lower()

    if not mods:
        return _encode_unmodified(lower)

    if lower in _CURSOR_KEYS:
        param = _xterm_modifier_param(shift, alt, ctrl)
        return f"{ESC}[1;{param}{_CURSOR_KEYS[lower]}".encode()
    if lower in _TILDE_KEYS:
        param = _xterm_modifier_param(shift, alt, ctrl)
        return f"{ESC}[{_TILDE_KEYS[lower]};{param}~".encode()
    if lower in _SS3_KEYS:
        param = _xterm_modifier_param(shift, alt, ctrl)
        return f"{ESC}[1;{param}{_SS3_KEYS[lower]}".encode()

    if lower == "tab" and shift and not ctrl:
        seq = f"{ESC}[Z"
        return (ESC + seq if alt else seq).encode()

    base = _encode_char_with_ctrl_shift(key, lower, ctrl, shift, spec)
    if alt:
        base = ESC + base
    return base.encode("utf-8")


def _encode_unmodified(lower: str) -> Optional[bytes]:
    if lower in _PLAIN_KEYS:
        return _PLAIN_KEYS[lower].encode()
    if lower in _CURSOR_KEYS:
        return f"{ESC}[{_CURSOR_KEYS[lower]}".encode()
    if lower in _TILDE_KEYS:
        return f"{ESC}[{_TILDE_KEYS[lower]}~".encode()
    if lower in _SS3_KEYS:
        return f"{ESC}O{_SS3_KEYS[lower]}".encode()
    return None


def _encode_char_with_ctrl_shift(key: str, lower: str, ctrl: bool, shift: bool,
                                 spec: str) -> str:
    """Encode a printable or plain key that may carry ctrl and/or shift."""
    if lower in _PLAIN_KEYS:
        if ctrl and lower == "space":
            return "\x00"
        if ctrl or shift:
            if lower in ("enter", "return", "backspace", "bs", "esc", "escape"):
                # Legacy encoding has no distinct form, send the plain key.
                return _PLAIN_KEYS[lower]
            raise KeySpecError(f"cannot encode {spec!r} in legacy key encoding")
        return _PLAIN_KEYS[lower]

    if len(key) != 1:
        raise KeySpecError(f"unknown key {key!r} in key spec {spec!r}")

    char = key
    if ctrl:
        if shift and char.isalpha():
            raise KeySpecError(
                f"{spec!r} has no legacy encoding (ctrl+shift+letter equals ctrl+letter)")
        if char.isalpha():
            return chr(ord(char.lower()) & 0x1F)
        if char in _CTRL_PUNCT:
            return _CTRL_PUNCT[char]
        raise KeySpecError(f"cannot encode {spec!r}: no control character for {char!r}")
    if shift:
        return char.upper()
    return char


def parse_sequence(text: str) -> List[str]:
    """Split a CLI key string like ``"J J ctrl+k 'two words'"`` into tokens.

    Quoting follows shell rules, so a quoted token can contain spaces and is
    typed literally by Session.send when it is not a key name.
    """
    return shlex.split(text)
