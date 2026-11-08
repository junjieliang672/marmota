"""Render a ScreenSnapshot to a PNG with Pillow.

* Text uses a monospace system font (Menlo, then SF Mono / Monaco on macOS,
  DejaVu / Liberation Mono on Linux, $SPF_TUI_FONT to override). Glyphs the
  font lacks fall back per character to a Nerd Font (for superfile's icons),
  symbol and CJK fonts, then color emoji, then a "tofu" box.
* Box drawing (U+2500-257F), block elements (U+2580-259F) and braille
  (U+2800-28FF) are drawn as shapes, so borders join and half-block image
  previews tile without gaps, as in a real terminal.
* Wide characters span two cells. Reverse video, bold, italic, underline and
  strikethrough are honored. The cursor is drawn as a block when visible.
"""
from __future__ import annotations

import functools
import glob
import os
from dataclasses import dataclass, field
from pathlib import Path
from typing import Dict, List, Optional, Sequence, Tuple, Union

from PIL import Image, ImageDraw, ImageFont

from tui.screen import Cell, ScreenSnapshot
from tui.terminal import hex_to_rgb

Font = Union[ImageFont.FreeTypeFont, ImageFont.ImageFont]
RGB = Tuple[int, int, int]

DEFAULT_FONT_SIZE: int = 16
LINE_HEIGHT: float = 1.25

# (path, face indexes for regular, bold, italic, bold italic) or separate files.
_MONO_FONTS: Sequence[Tuple[str, Tuple[Union[int, str], ...]]] = (
    ("/System/Library/Fonts/Menlo.ttc", (0, 1, 2, 3)),
    ("/System/Library/Fonts/SFNSMono.ttf", (0, 0, 0, 0)),
    ("/System/Library/Fonts/Monaco.ttf", (0, 0, 0, 0)),
    ("/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
     ("", "DejaVuSansMono-Bold.ttf", "DejaVuSansMono-Oblique.ttf",
      "DejaVuSansMono-BoldOblique.ttf")),
    ("/usr/share/fonts/TTF/DejaVuSansMono.ttf",
     ("", "DejaVuSansMono-Bold.ttf", "DejaVuSansMono-Oblique.ttf",
      "DejaVuSansMono-BoldOblique.ttf")),
    ("/usr/share/fonts/truetype/liberation/LiberationMono-Regular.ttf",
     ("", "LiberationMono-Bold.ttf", "LiberationMono-Italic.ttf",
      "LiberationMono-BoldItalic.ttf")),
)

_NERD_FONT_GLOBS: Sequence[str] = (
    "~/Library/Fonts/*NerdFontMono-Regular.ttf",
    "/Library/Fonts/*NerdFontMono-Regular.ttf",
    "~/.local/share/fonts/**/*NerdFontMono-Regular.ttf",
    "/usr/share/fonts/**/*NerdFontMono-Regular.ttf",
    "~/Library/Fonts/SymbolsNerdFontMono-Regular.ttf",
    "~/Library/Fonts/*NerdFont-Regular.ttf",
    "/usr/share/fonts/**/*NerdFont-Regular.ttf",
)

_FALLBACK_FONTS: Sequence[str] = (
    "/System/Library/Fonts/Apple Symbols.ttf",
    "/System/Library/Fonts/Hiragino Sans GB.ttc",
    "/Library/Fonts/Arial Unicode.ttf",
    "/System/Library/Fonts/Supplemental/Arial Unicode.ttf",
    "/System/Library/Fonts/STHeiti Light.ttc",
    "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
    "/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
    "/usr/share/fonts/noto-cjk/NotoSansCJK-Regular.ttc",
)

_EMOJI_FONTS: Sequence[Tuple[str, Sequence[int]]] = (
    ("/System/Library/Fonts/Apple Color Emoji.ttc", (64, 48, 40, 32, 96, 160, 20)),
    ("/usr/share/fonts/truetype/noto/NotoColorEmoji.ttf", (109,)),
    ("/usr/share/fonts/noto/NotoColorEmoji.ttf", (109,)),
)

_MISSING_PROBE = "\U0010fffd"


def _truetype(path: str, size: int, index: int = 0) -> Optional[ImageFont.FreeTypeFont]:
    try:
        return ImageFont.truetype(path, size, index=index)
    except OSError:
        return None


def _load_mono(size: int) -> Tuple[Font, Font, Font, Font]:
    override = os.environ.get("SPF_TUI_FONT")
    if override:
        font = _truetype(override, size)
        if font is not None:
            bold = _truetype(os.environ.get("SPF_TUI_FONT_BOLD", ""), size) or font
            return font, bold, font, bold
    for path, faces in _MONO_FONTS:
        if not os.path.exists(path):
            continue
        regular = _truetype(path, size, faces[0] if isinstance(faces[0], int) else 0)
        if regular is None:
            continue
        styles: List[Font] = [regular]
        for face in faces[1:]:
            font: Optional[Font] = None
            if isinstance(face, int):
                font = _truetype(path, size, face)
            elif face:
                font = _truetype(os.path.join(os.path.dirname(path), face), size)
            styles.append(font or regular)
        return styles[0], styles[1], styles[2], styles[3]
    default = ImageFont.load_default(size)
    return default, default, default, default


def _find_nerd_font() -> Optional[str]:
    for pattern in _NERD_FONT_GLOBS:
        matches = sorted(glob.glob(os.path.expanduser(pattern), recursive=True))
        if matches:
            return matches[0]
    return None


@dataclass
class FontSet:
    """Fonts and cell metrics used by render_png (see load_fonts)."""

    size: int
    styles: Tuple[Font, Font, Font, Font]
    fallbacks: List[Font]
    emoji: Optional[Tuple[ImageFont.FreeTypeFont, int]]
    cell_width: int
    cell_height: int
    baseline: int
    _notdef: Dict[int, Tuple[Tuple[int, int], bytes]] = field(default_factory=dict)
    _coverage: Dict[Tuple[int, str], bool] = field(default_factory=dict)

    def style(self, bold: bool, italic: bool) -> Font:
        return self.styles[(1 if bold else 0) + (2 if italic else 0)]

    def has_glyph(self, font: Font, text: str) -> bool:
        """Heuristic: a glyph is missing if it renders exactly like .notdef."""
        key = (id(font), text)
        cached = self._coverage.get(key)
        if cached is not None:
            return cached
        notdef = self._notdef.get(id(font))
        if notdef is None:
            mask = font.getmask(_MISSING_PROBE)
            notdef = (mask.size, bytes(mask))
            self._notdef[id(font)] = notdef
        mask = font.getmask(text)
        present = (mask.size, bytes(mask)) != notdef
        self._coverage[key] = present
        return present

    def pick(self, text: str, bold: bool, italic: bool) -> Optional[Font]:
        primary = self.style(bold, italic)
        if self.has_glyph(primary, text):
            return primary
        if (bold or italic) and self.has_glyph(self.styles[0], text):
            return self.styles[0]
        for font in self.fallbacks:
            if self.has_glyph(font, text):
                return font
        return None


@functools.lru_cache(maxsize=8)
def load_fonts(size: int = DEFAULT_FONT_SIZE) -> FontSet:
    styles = _load_mono(size)
    fallbacks: List[Font] = []
    nerd = _find_nerd_font()
    for path in ([nerd] if nerd else []) + list(_FALLBACK_FONTS):
        if path and os.path.exists(path):
            font = _truetype(path, size)
            if font is not None:
                fallbacks.append(font)
    emoji: Optional[Tuple[ImageFont.FreeTypeFont, int]] = None
    for path, sizes in _EMOJI_FONTS:
        if not os.path.exists(path):
            continue
        for strike in sizes:
            font = _truetype(path, strike)
            if font is not None:
                emoji = (font, strike)
                break
        if emoji is not None:
            break
    regular = styles[0]
    if isinstance(regular, ImageFont.FreeTypeFont):
        ascent, descent = regular.getmetrics()
        advance = regular.getlength("M")
    else:
        ascent, descent, advance = size, size // 4, size * 0.6
    cell_width = max(1, round(advance))
    cell_height = max(ascent + descent, round(size * LINE_HEIGHT))
    baseline = (cell_height - (ascent + descent)) // 2 + ascent
    return FontSet(size, styles, fallbacks, emoji, cell_width, cell_height, baseline)


# ----- procedural glyphs -------------------------------------------------------

# Arms (up, right, down, left): 0 none, 1 light, 2 heavy, 3 double.
_BOX_ARMS: Dict[int, str] = {
    0x2500: "0101", 0x2501: "0202", 0x2502: "1010", 0x2503: "2020",
    0x250C: "0110", 0x250D: "0210", 0x250E: "0120", 0x250F: "0220",
    0x2510: "0011", 0x2511: "0012", 0x2512: "0021", 0x2513: "0022",
    0x2514: "1100", 0x2515: "1200", 0x2516: "2100", 0x2517: "2200",
    0x2518: "1001", 0x2519: "1002", 0x251A: "2001", 0x251B: "2002",
    0x251C: "1110", 0x251D: "1210", 0x251E: "2110", 0x251F: "1120",
    0x2520: "2120", 0x2521: "2210", 0x2522: "1220", 0x2523: "2220",
    0x2524: "1011", 0x2525: "1012", 0x2526: "2011", 0x2527: "1021",
    0x2528: "2021", 0x2529: "2012", 0x252A: "1022", 0x252B: "2022",
    0x252C: "0111", 0x252D: "0112", 0x252E: "0211", 0x252F: "0212",
    0x2530: "0121", 0x2531: "0122", 0x2532: "0221", 0x2533: "0222",
    0x2534: "1101", 0x2535: "1102", 0x2536: "1201", 0x2537: "1202",
    0x2538: "2101", 0x2539: "2102", 0x253A: "2201", 0x253B: "2202",
    0x253C: "1111", 0x253D: "1112", 0x253E: "1211", 0x253F: "1212",
    0x2540: "2111", 0x2541: "1121", 0x2542: "2121", 0x2543: "2112",
    0x2544: "2211", 0x2545: "1122", 0x2546: "1221", 0x2547: "2212",
    0x2548: "1222", 0x2549: "2122", 0x254A: "2221", 0x254B: "2222",
    0x2550: "0303", 0x2551: "3030", 0x2552: "0310", 0x2553: "0130",
    0x2554: "0330", 0x2555: "0013", 0x2556: "0031", 0x2557: "0033",
    0x2558: "1300", 0x2559: "3100", 0x255A: "3300", 0x255B: "1003",
    0x255C: "3001", 0x255D: "3003", 0x255E: "1310", 0x255F: "3130",
    0x2560: "3330", 0x2561: "1013", 0x2562: "3031", 0x2563: "3033",
    0x2564: "0313", 0x2565: "0131", 0x2566: "0333", 0x2567: "1303",
    0x2568: "3101", 0x2569: "3303", 0x256A: "1313", 0x256B: "3131",
    0x256C: "3333",
    0x2574: "0001", 0x2575: "1000", 0x2576: "0100", 0x2577: "0010",
    0x2578: "0002", 0x2579: "2000", 0x257A: "0200", 0x257B: "0020",
    0x257C: "0201", 0x257D: "1020", 0x257E: "0102", 0x257F: "2010",
}

# Dashed lines: codepoint -> (vertical, weight, dash count).
_BOX_DASHES: Dict[int, Tuple[bool, int, int]] = {
    0x2504: (False, 1, 3), 0x2505: (False, 2, 3), 0x2506: (True, 1, 3), 0x2507: (True, 2, 3),
    0x2508: (False, 1, 4), 0x2509: (False, 2, 4), 0x250A: (True, 1, 4), 0x250B: (True, 2, 4),
    0x254C: (False, 1, 2), 0x254D: (False, 2, 2), 0x254E: (True, 1, 2), 0x254F: (True, 2, 2),
}

# Quadrant blocks: bit 1 upper-left, 2 upper-right, 4 lower-left, 8 lower-right.
_QUADRANTS: Dict[int, int] = {
    0x2596: 4, 0x2597: 8, 0x2598: 1, 0x2599: 13, 0x259A: 9, 0x259B: 7, 0x259C: 11,
    0x259D: 2, 0x259E: 6, 0x259F: 14,
}

_SHADES: Dict[int, float] = {0x2591: 0.25, 0x2592: 0.5, 0x2593: 0.75}

# Braille dot bit -> (column, row).
_BRAILLE_DOTS: Sequence[Tuple[int, int, int]] = (
    (0, 0, 0), (1, 0, 1), (2, 0, 2), (3, 1, 0), (4, 1, 1), (5, 1, 2), (6, 0, 3), (7, 1, 3))


class _Painter:
    def __init__(self, image: Image.Image, fonts: FontSet) -> None:
        self.image = image
        self.draw = ImageDraw.Draw(image)
        self.fonts = fonts
        self.width = fonts.cell_width
        self.height = fonts.cell_height
        self.light = max(1, round(fonts.size / 16))
        self.heavy = self.light * 2 + 1

    def rect(self, x0: int, y0: int, x1: int, y1: int, color: RGB) -> None:
        """Fill [x0, x1) x [y0, y1)."""
        if x1 > x0 and y1 > y0:
            self.draw.rectangle((x0, y0, x1 - 1, y1 - 1), fill=color)

    def shape(self, codepoint: int, x: int, y: int, fg: RGB, bg: RGB) -> bool:
        """Draw box/block/braille glyphs; return False if codepoint is not one."""
        if codepoint in _BOX_ARMS:
            self._box(_BOX_ARMS[codepoint], x, y, fg)
        elif codepoint in _BOX_DASHES:
            self._dashes(*_BOX_DASHES[codepoint], x=x, y=y, color=fg)
        elif 0x256D <= codepoint <= 0x2570:
            self._arc(codepoint, x, y, fg)
        elif 0x2571 <= codepoint <= 0x2573:
            self._diagonal(codepoint, x, y, fg)
        elif 0x2580 <= codepoint <= 0x259F:
            self._block(codepoint, x, y, fg, bg)
        elif 0x2800 <= codepoint <= 0x28FF:
            self._braille(codepoint - 0x2800, x, y, fg)
        else:
            return False
        return True

    def _thickness(self, weight: int) -> int:
        return self.heavy if weight == 2 else self.light

    def _box(self, arms: str, x: int, y: int, color: RGB) -> None:
        w, h = self.width, self.height
        cx, cy = x + w // 2, y + h // 2
        up, right, down, left = (int(a) for a in arms)
        gap = self.light + 1
        for weight, vertical, toward_start in ((up, True, True), (down, True, False),
                                               (left, False, True), (right, False, False)):
            if weight == 0:
                continue
            offsets = (-gap, gap) if weight == 3 else (0,)
            t = self._thickness(weight)
            # Arms overshoot the center so that junctions fill in.
            reach = max(self._thickness(int(a)) for a in arms) // 2 + (gap if weight == 3 else 0)
            for offset in offsets:
                if vertical:
                    lx = cx + offset - t // 2
                    if toward_start:
                        self.rect(lx, y, lx + t, cy + reach + 1, color)
                    else:
                        self.rect(lx, cy - reach, lx + t, y + h, color)
                else:
                    ly = cy + offset - t // 2
                    if toward_start:
                        self.rect(x, ly, cx + reach + 1, ly + t, color)
                    else:
                        self.rect(cx - reach, ly, x + w, ly + t, color)

    def _dashes(self, vertical: bool, weight: int, dashes: int, x: int, y: int,
                color: RGB) -> None:
        w, h = self.width, self.height
        t = self._thickness(weight)
        length = h if vertical else w
        step = length / dashes
        for index in range(dashes):
            start = round(index * step)
            end = round(index * step + step * 0.6)
            if vertical:
                lx = x + w // 2 - t // 2
                self.rect(lx, y + start, lx + t, y + end, color)
            else:
                ly = y + h // 2 - t // 2
                self.rect(x + start, ly, x + end, ly + t, color)

    def _arc(self, codepoint: int, x: int, y: int, color: RGB) -> None:
        w, h = self.width, self.height
        cx, cy = x + w // 2, y + h // 2
        t = self.light
        r = max(1, min(w, h) // 2)
        lx, ly = cx - t // 2, cy - t // 2
        if codepoint == 0x256D:    # down and right
            self.draw.arc((cx, cy, cx + 2 * r, cy + 2 * r), 180, 270, fill=color, width=t)
            self.rect(lx, cy + r, lx + t, y + h, color)
            self.rect(cx + r, ly, x + w, ly + t, color)
        elif codepoint == 0x256E:  # down and left
            self.draw.arc((cx - 2 * r, cy, cx, cy + 2 * r), 270, 360, fill=color, width=t)
            self.rect(lx, cy + r, lx + t, y + h, color)
            self.rect(x, ly, cx - r + 1, ly + t, color)
        elif codepoint == 0x256F:  # up and left
            self.draw.arc((cx - 2 * r, cy - 2 * r, cx, cy), 0, 90, fill=color, width=t)
            self.rect(lx, y, lx + t, cy - r + 1, color)
            self.rect(x, ly, cx - r + 1, ly + t, color)
        else:                      # up and right
            self.draw.arc((cx, cy - 2 * r, cx + 2 * r, cy), 90, 180, fill=color, width=t)
            self.rect(lx, y, lx + t, cy - r + 1, color)
            self.rect(cx + r, ly, x + w, ly + t, color)

    def _diagonal(self, codepoint: int, x: int, y: int, color: RGB) -> None:
        w, h = self.width, self.height
        if codepoint in (0x2571, 0x2573):
            self.draw.line((x + w - 1, y, x, y + h - 1), fill=color, width=self.light)
        if codepoint in (0x2572, 0x2573):
            self.draw.line((x, y, x + w - 1, y + h - 1), fill=color, width=self.light)

    def _block(self, codepoint: int, x: int, y: int, fg: RGB, bg: RGB) -> None:
        w, h = self.width, self.height
        if codepoint == 0x2580:
            self.rect(x, y, x + w, y + h // 2, fg)
        elif 0x2581 <= codepoint <= 0x2588:
            eighths = codepoint - 0x2580
            self.rect(x, y + h - round(h * eighths / 8), x + w, y + h, fg)
        elif 0x2589 <= codepoint <= 0x258F:
            eighths = 0x2590 - codepoint
            self.rect(x, y, x + round(w * eighths / 8), y + h, fg)
        elif codepoint == 0x2590:
            self.rect(x + w // 2, y, x + w, y + h, fg)
        elif codepoint in _SHADES:
            alpha = _SHADES[codepoint]
            mixed = tuple(round(f * alpha + b * (1 - alpha)) for f, b in zip(fg, bg))
            self.rect(x, y, x + w, y + h, mixed)  # type: ignore[arg-type]
        elif codepoint == 0x2594:
            self.rect(x, y, x + w, y + max(1, round(h / 8)), fg)
        elif codepoint == 0x2595:
            self.rect(x + w - max(1, round(w / 8)), y, x + w, y + h, fg)
        else:
            bits = _QUADRANTS[codepoint]
            hx, hy = w // 2, h // 2
            if bits & 1:
                self.rect(x, y, x + hx, y + hy, fg)
            if bits & 2:
                self.rect(x + hx, y, x + w, y + hy, fg)
            if bits & 4:
                self.rect(x, y + hy, x + hx, y + h, fg)
            if bits & 8:
                self.rect(x + hx, y + hy, x + w, y + h, fg)

    def _braille(self, bits: int, x: int, y: int, color: RGB) -> None:
        w, h = self.width, self.height
        dot = max(1, round(w / 5))
        for bit, column, row in _BRAILLE_DOTS:
            if bits & (1 << bit):
                dx = x + round(w * (1 + 2 * column) / 4) - dot // 2
                dy = y + round(h * (1 + 2 * row) / 8) - dot // 2
                self.rect(dx, dy, dx + dot, dy + dot, color)

    def text(self, text: str, x: int, y: int, span: int, fg: RGB, bold: bool,
             italic: bool) -> None:
        font = self.fonts.pick(text, bold, italic)
        center = x + span * self.width // 2
        if font is not None:
            self.draw.text((center, y + self.fonts.baseline), text, font=font, fill=fg,
                           anchor="ms")
            return
        if self.fonts.emoji is not None and self._emoji(text, x, y, span):
            return
        # Tofu box for glyphs no font has.
        self.draw.rectangle((x + 1, y + 2, x + span * self.width - 2, y + self.height - 3),
                            outline=fg)

    def _emoji(self, text: str, x: int, y: int, span: int) -> bool:
        assert self.fonts.emoji is not None
        font, strike = self.fonts.emoji
        canvas = Image.new("RGBA", (strike * 2, strike * 2), (0, 0, 0, 0))
        try:
            ImageDraw.Draw(canvas).text((0, 0), text, font=font, embedded_color=True)
        except (OSError, ValueError):
            return False
        box = canvas.getbbox()
        if box is None:
            return False
        glyph = canvas.crop(box)
        target_h = self.height - 2
        target_w = span * self.width
        scale = min(target_w / glyph.width, target_h / glyph.height)
        size = (max(1, round(glyph.width * scale)), max(1, round(glyph.height * scale)))
        glyph = glyph.resize(size, Image.LANCZOS)
        self.image.paste(glyph, (x + (target_w - size[0]) // 2, y + (self.height - size[1]) // 2),
                         glyph)
        return True


def render_png(snapshot: ScreenSnapshot, path: Union[str, "os.PathLike[str]"], *,
               font_size: int = DEFAULT_FONT_SIZE, padding: int = 8,
               cursor: str = "auto") -> Path:
    """Render snapshot to a PNG file and return its path.

    Args:
        font_size: pixel size of the font; 16 gives 10x20 px cells.
        padding: margin around the grid, in the terminal background color.
        cursor: "auto" draws the cursor when the program shows it, "always"
            or "never" override that.
    """
    fonts = load_fonts(font_size)
    palette = snapshot.palette
    width = snapshot.cols * fonts.cell_width + 2 * padding
    height = snapshot.rows * fonts.cell_height + 2 * padding
    image = Image.new("RGB", (width, height), hex_to_rgb(palette.background))
    painter = _Painter(image, fonts)
    show_cursor = cursor == "always" or (cursor == "auto" and snapshot.cursor_visible)
    for row_index, row in enumerate(snapshot.cells):
        y = padding + row_index * fonts.cell_height
        for col_index, cell in enumerate(row):
            if cell.stub and col_index > 0 and row[col_index - 1].wide:
                continue
            is_cursor = show_cursor and (row_index, col_index) == snapshot.cursor
            _paint_cell(painter, cell, padding + col_index * fonts.cell_width, y, is_cursor,
                        hex_to_rgb(palette.cursor))
    target = Path(path)
    target.parent.mkdir(parents=True, exist_ok=True)
    image.save(target)
    return target


def _paint_cell(painter: _Painter, cell: Cell, x: int, y: int, is_cursor: bool,
                cursor_color: RGB) -> None:
    fg_hex, bg_hex = cell.visible_colors()
    fg, bg = hex_to_rgb(fg_hex), hex_to_rgb(bg_hex)
    if is_cursor:
        fg, bg = bg, cursor_color
    span = 2 if cell.wide else 1
    painter.rect(x, y, x + span * painter.width, y + painter.height, bg)
    text = cell.char
    if text and not text.isspace():
        if not painter.shape(ord(text[0]), x, y, fg, bg):
            painter.text(text, x, y, span, fg, cell.bold, cell.italic)
    if cell.underline:
        line_y = y + painter.fonts.baseline + 2
        painter.rect(x, line_y, x + span * painter.width, line_y + painter.light, fg)
    if cell.strikethrough:
        line_y = y + painter.height // 2
        painter.rect(x, line_y, x + span * painter.width, line_y + painter.light, fg)
