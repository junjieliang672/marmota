"""pty + pyte harness that drives the real spf binary without tmux. See README.md.

PIL is only needed for screenshots and is imported lazily (tui.screenshot).
"""
from tui.keys import KeySpecError, encode_key, parse_sequence
from tui.sandbox import Sandbox, claude_project_slug
from tui.screen import Cell, ScreenSnapshot
from tui.session import Session, SessionExited, WaitTimeout, wait_until
from tui.terminal import Palette

__all__ = [
    "Cell",
    "KeySpecError",
    "Palette",
    "Sandbox",
    "ScreenSnapshot",
    "Session",
    "SessionExited",
    "WaitTimeout",
    "claude_project_slug",
    "encode_key",
    "parse_sequence",
    "wait_until",
]
