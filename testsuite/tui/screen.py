"""Immutable views of the emulated screen.

Coordinates are (row, col), 0-based, in terminal cells. A wide character (CJK,
most emoji) occupies its cell plus a "stub" cell to its right; the stub has
``stub=True`` and an empty ``char``. Text helpers skip stubs, so a returned
line reads like copied terminal text, while ``find`` returns cell columns.
"""
from __future__ import annotations

import re
from dataclasses import dataclass
from typing import List, Optional, Pattern, Sequence, Tuple, Union

import pyte

from tui.terminal import Palette

Needle = Union[str, Pattern[str]]


@dataclass(frozen=True)
class Cell:
    """One screen cell. ``fg``/``bg`` are resolved "#rrggbb" colors.

    ``fg_raw``/``bg_raw`` keep pyte's value ("default", "red" or "rrggbb"),
    so a test can tell an explicit color from the terminal default.
    Reverse video is not applied to fg/bg; use ``visible_colors()``.
    """

    char: str
    fg: str
    bg: str
    fg_raw: str
    bg_raw: str
    bold: bool
    italic: bool
    underline: bool
    strikethrough: bool
    reverse: bool
    wide: bool
    stub: bool

    def visible_colors(self) -> Tuple[str, str]:
        """(fg, bg) as painted, i.e. with reverse video applied."""
        if self.reverse:
            return self.bg, self.fg
        return self.fg, self.bg


def cell_from_pyte(char: "pyte.screens.Char", palette: Palette, wide: bool) -> Cell:
    return Cell(
        char=char.data,
        fg=palette.resolve(char.fg, foreground=True),
        bg=palette.resolve(char.bg, foreground=False),
        fg_raw=char.fg,
        bg_raw=char.bg,
        bold=bool(char.bold),
        italic=bool(char.italics),
        underline=bool(char.underscore),
        strikethrough=bool(char.strikethrough),
        reverse=bool(char.reverse),
        wide=wide,
        stub=char.data == "",
    )


def row_cells(screen: pyte.Screen, row: int, palette: Palette) -> List[Cell]:
    line = screen.buffer[row]
    cells: List[Cell] = []
    for col in range(screen.columns):
        char = line[col]
        wide = bool(char.data) and col + 1 < screen.columns and line[col + 1].data == ""
        cells.append(cell_from_pyte(char, palette, wide))
    return cells


def index_row(datas: Sequence[str]) -> Tuple[str, List[int]]:
    """Join cell contents (stubs skipped) and map each code point to its column.

    pyte appends combining marks to the previous cell, so one cell can hold
    several code points; every one of them maps to that cell's column.
    """
    chars: List[str] = []
    columns: List[int] = []
    for col, data in enumerate(datas):
        if data == "":
            continue
        chars.append(data)
        columns.extend([col] * len(data))
    return "".join(chars), columns


def row_text(screen: pyte.Screen, row: int) -> Tuple[str, List[int]]:
    """Return the row's text (stubs skipped) and the cell column of each char."""
    line = screen.buffer[row]
    return index_row([line[col].data for col in range(screen.columns)])


def search_line(line: str, needle: Needle, start: int = 0) -> Optional[Tuple[int, int]]:
    """Return (start, end) string offsets of needle in line, or None."""
    if isinstance(needle, str):
        index = line.find(needle, start)
        if index < 0:
            return None
        return index, index + len(needle)
    match = needle.search(line, start)
    if match is None or match.end() == match.start():
        return None
    return match.start(), match.end()


def find_in_lines(lines: Sequence[Tuple[str, List[int]]],
                  needle: Needle) -> List[Tuple[int, int]]:
    """All (row, col) cell positions where needle starts."""
    hits: List[Tuple[int, int]] = []
    for row, (text, columns) in enumerate(lines):
        offset = 0
        while True:
            span = search_line(text, needle, offset)
            if span is None:
                break
            hits.append((row, _column_of(text, columns, span[0])))
            offset = span[0] + 1
    return hits


def _column_of(text: str, columns: List[int], offset: int) -> int:
    if offset < len(columns):
        return columns[offset]
    return columns[-1] + 1 if columns else len(text)


@dataclass(frozen=True)
class ScreenSnapshot:
    """A full copy of the screen at one moment, safe to keep and render."""

    cols: int
    rows: int
    cells: Tuple[Tuple[Cell, ...], ...]
    cursor: Tuple[int, int]
    cursor_visible: bool
    title: str
    palette: Palette

    def lines(self) -> List[str]:
        return ["".join(cell.char for cell in row if not cell.stub) for row in self.cells]

    def text(self) -> str:
        return "\n".join(line.rstrip() for line in self.lines())

    def find(self, needle: Needle) -> Optional[Tuple[int, int]]:
        hits = self.find_all(needle)
        return hits[0] if hits else None

    def find_all(self, needle: Needle) -> List[Tuple[int, int]]:
        indexed = [index_row([cell.char for cell in row]) for row in self.cells]
        return find_in_lines(indexed, needle)


def compile_needle(needle: Needle) -> Needle:
    """Accept a plain string or a compiled regex (re.compile)."""
    if isinstance(needle, str):
        return needle
    if isinstance(needle, re.Pattern):
        return needle
    raise TypeError(f"needle must be str or re.Pattern, got {type(needle).__name__}")


def snapshot_from_pyte(screen: pyte.Screen, palette: Palette, title: str) -> ScreenSnapshot:
    rows = tuple(tuple(row_cells(screen, row, palette)) for row in range(screen.lines))
    return ScreenSnapshot(
        cols=screen.columns,
        rows=screen.lines,
        cells=rows,
        cursor=(screen.cursor.y, min(screen.cursor.x, screen.columns - 1)),
        cursor_visible=not screen.cursor.hidden,
        title=title,
        palette=palette,
    )
