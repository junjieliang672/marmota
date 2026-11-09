#!/usr/bin/env python3
"""Draws a capture (out/NAME.json) as the WezTerm window it came from, to PNG
(out/NAME.png), with headless Chrome.

    render.py NAME [NAME...]

It looks like WezTerm as build.sh sets it up: Tokyo Night, JetBrains Mono 14,
the blue line between panes, inactive panes at 0.7 brightness, the blue window
edge beside the active pane.

Nothing about this computer reaches the picture: your user name becomes one as
long, memory and disk are scaled to a 16 GB / 512 GB Mac (the bars still
match), and subs.json holds more [pattern, replacement] pairs, each replacement
kept to the length of what it replaces.
"""
import html
import json
import os
import re
import subprocess
import sys
import unicodedata
from pathlib import Path

HERE = Path(__file__).resolve().parent
OUT = HERE / "out"
CHROME = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"

FONT_PX = 14
CW = FONT_PX * 0.6           # JetBrains Mono advance
LH = round(FONT_PX * 1.32, 2)  # ascent + descent
PAD_X, PAD_Y = CW, LH / 2     # WezTerm's default window padding
EDGE_X, EDGE_Y = 0.4 * CW, 0.2 * LH
BLUE = "#7aa2f7"

FG, BG = "#c0caf5", "#1a1b26"
ANSI = ["#15161e", "#f7768e", "#9ece6a", "#e0af68", "#7aa2f7", "#bb9af7", "#7dcfff", "#a9b1d6",
        "#414868", "#f7768e", "#9ece6a", "#e0af68", "#7aa2f7", "#bb9af7", "#7dcfff", "#c0caf5"]


def color256(n):
    if n < 16:
        return ANSI[n]
    if n < 232:
        n -= 16
        steps = [0, 95, 135, 175, 215, 255]
        return "#%02x%02x%02x" % (steps[n // 36], steps[n // 6 % 6], steps[n % 6])
    v = 8 + (n - 232) * 10
    return "#%02x%02x%02x" % (v, v, v)


def mix(a, b, t):
    pa = [int(a[i:i + 2], 16) for i in (1, 3, 5)]
    pb = [int(b[i:i + 2], 16) for i in (1, 3, 5)]
    return "#%02x%02x%02x" % tuple(round(x * t + y * (1 - t)) for x, y in zip(pa, pb))


TOKEN = re.compile(r"\x1b\[([0-9;:?<>=]*)([@-~])|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[()*+].|\x1b.")


class Style:
    def __init__(self):
        self.reset()

    def reset(self):
        self.fg = self.bg = None
        self.fg_idx = None
        self.bold = self.dim = self.italic = self.under = self.rev = self.strike = False

    def key(self):
        return (self.fg, self.fg_idx, self.bg, self.bold, self.dim, self.italic, self.under, self.rev, self.strike)

    def apply(self, params):
        parts = [p for p in re.split(r";", params)] if params else ["0"]
        i = 0
        while i < len(parts):
            p = parts[i]
            sub = p.split(":")
            code = int(sub[0]) if sub[0].isdigit() else 0
            if code in (38, 48, 58):
                if len(sub) > 1:  # colon form: 38:2::r:g:b or 38:2:r:g:b or 38:5:n
                    vals = [s for s in sub[1:]]
                    if vals[0] == "5":
                        col = color256(int(vals[1]))
                        idx = int(vals[1])
                    else:
                        rgb = [int(v) for v in vals[1:] if v != ""][-3:]
                        col, idx = "#%02x%02x%02x" % tuple(rgb), None
                else:
                    if parts[i + 1] == "5":
                        col, idx = color256(int(parts[i + 2])), int(parts[i + 2])
                        i += 2
                    else:
                        col, idx = "#%02x%02x%02x" % tuple(int(x) for x in parts[i + 2:i + 5]), None
                        i += 4
                if code == 38:
                    self.fg, self.fg_idx = col, idx
                elif code == 48:
                    self.bg = col
            elif code == 0:
                self.reset()
            elif code == 1:
                self.bold = True
            elif code == 2:
                self.dim = True
            elif code == 3:
                self.italic = True
            elif code == 4:
                self.under = not (len(sub) > 1 and sub[1] == "0")
            elif code == 7:
                self.rev = True
            elif code == 9:
                self.strike = True
            elif code == 22:
                self.bold = self.dim = False
            elif code == 23:
                self.italic = False
            elif code == 24:
                self.under = False
            elif code == 27:
                self.rev = False
            elif code == 29:
                self.strike = False
            elif 30 <= code <= 37:
                self.fg, self.fg_idx = ANSI[code - 30], code - 30
            elif code == 39:
                self.fg = self.fg_idx = None
            elif 40 <= code <= 47:
                self.bg = ANSI[code - 40]
            elif code == 49:
                self.bg = None
            elif 90 <= code <= 97:
                self.fg, self.fg_idx = ANSI[code - 82], code - 82
            elif 100 <= code <= 107:
                self.bg = ANSI[code - 92]
            i += 1


def width(ch):
    if unicodedata.combining(ch):
        return 0
    return 2 if unicodedata.east_asian_width(ch) in ("W", "F") else 1


def parse(text):
    """Lines of cells: (char, style key, width)."""
    st = Style()
    lines = []
    for raw in text.split("\n"):
        cells = []
        pos = 0
        for m in TOKEN.finditer(raw):
            for ch in raw[pos:m.start()]:
                cells.append((ch, st.key(), width(ch)))
            if m.group(2) == "m":
                st.apply(m.group(1))
            pos = m.end()
        for ch in raw[pos:]:
            cells.append((ch, st.key(), width(ch)))
        lines.append(cells)
    return lines


MOCK_TOTALS = {"memory": 16, "disk": 512}


def mock_machine(text):
    for label, total in MOCK_TOTALS.items():
        m = re.search(label + r"(.*?)(\d+(?:\.\d+)?)( / )(\d+)( GB)", text)
        if not m:
            continue
        used = float(m.group(2)) * total / float(m.group(4))
        old = m.group(2) + m.group(3) + m.group(4)
        new = f"{used:.0f} / {total}" if used >= 10 or len(old) < 8 else f"{used:.1f} / {total}"
        new = new.rjust(len(old))[:len(old)]
        text = text[:m.start(2)] + new + text[m.start(2) + len(old):]
    return text


def substitute(cells, subs):
    text = mock_machine("".join(c[0] for c in cells))
    for pat, rep in subs:
        for m in list(re.finditer(pat, text)):
            r = m.expand(rep)
            n = m.end() - m.start()
            r = (r + " " * n)[:n]
            text = text[:m.start()] + r + text[m.end():]
    return [(text[i], c[1], width(text[i])) for i, c in enumerate(cells)]


# Block characters drawn as the terminal does: quadrants filling the cell.
# (upper left, upper right, lower left, lower right)
QUADS = {
    "█": "1111", "▀": "1100", "▄": "0011", "▌": "1010", "▐": "0101",
    "▖": "0010", "▗": "0001", "▘": "1000", "▝": "0100", "▙": "1011", "▚": "1001",
    "▛": "1110", "▜": "1101", "▞": "0110", "▟": "0111",
}


def quad_html(ch, w, fg):
    q = QUADS[ch]
    cells = "".join(f'<b style="background:{fg if on == "1" else "transparent"}"></b>' for on in q)
    return f'<i class="quad" style="width:{w * CW:.2f}px">{cells}</i>'


def colors(key):
    fg, fg_idx, bg, bold, dim, _, _, rev, _ = key
    if fg_idx is not None and fg_idx < 8 and bold:
        fg = ANSI[fg_idx + 8]
    fg = fg or FG
    bgc = bg or BG
    if rev:
        fg, bgc = bgc, fg
    if dim:
        fg = mix(fg, bgc, 0.55)
    return fg, (bgc if (bg or rev) else None)


def pane_html(p, subs):
    lines = parse(p["text"])[:p["rows"]]
    out = []
    for y, cells in enumerate(lines):
        cells = substitute(cells, subs)
        x = 0
        runs = []
        for ch, key, w in cells:
            if runs and runs[-1][1] == key:
                runs[-1][2].append((ch, w))
            else:
                runs.append([x, key, [(ch, w)]])
            x += w
        spans = []
        for x0, key, chars in runs:
            fg, bg = colors(key)
            _, _, _, bold, _, italic, under, _, strike = key
            n = sum(w for _, w in chars)
            if n == 0:
                continue
            css = [f"left:{x0 * CW:.2f}px", f"width:{n * CW:.2f}px", f"color:{fg}"]
            if bg:
                css.append(f"background:{bg}")
            if bold:
                css.append("font-weight:700")
            if italic:
                css.append("font-style:italic")
            deco = [d for d, on in (("underline", under), ("line-through", strike)) if on]
            if deco:
                css.append("text-decoration:" + " ".join(deco))
            body = []
            for ch, w in chars:
                if w == 0:
                    continue
                if ord(ch) < 128:
                    body.append(html.escape(ch))
                elif ch in QUADS:
                    body.append(quad_html(ch, w, fg))
                elif 0x2500 <= ord(ch) <= 0x259F:
                    body.append(f'<i class="box" style="width:{w * CW:.2f}px">{html.escape(ch)}</i>')
                else:
                    body.append(f'<i style="width:{w * CW:.2f}px">{html.escape(ch)}</i>')
            if all(ch == " " for ch, _ in chars) and not bg:
                continue
            spans.append(f'<span style="{";".join(css)}">{"".join(body)}</span>')
        out.append(f'<div class="row" style="top:{y * LH:.2f}px">{"".join(spans)}</div>')
    style = (f"left:{PAD_X + p["left"] * CW:.2f}px;top:{PAD_Y + p["top"] * LH:.2f}px;"
             f"width:{p['cols'] * CW:.2f}px;height:{p['rows'] * LH:.2f}px")
    cls = "pane" + ("" if p["active"] else " dim")
    return f'<div class="{cls}" style="{style}">{"".join(out)}</div>'


def render(name, subs):
    panes = json.loads((OUT / f"{name}.json").read_text())
    cols = max(p["left"] + p["cols"] for p in panes)
    rows = max(p["top"] + p["rows"] for p in panes)
    W = 2 * (PAD_X + EDGE_X) + cols * CW
    H = 2 * (PAD_Y + EDGE_Y) + rows * LH
    active = next((p for p in panes if p["active"]), panes[0])
    edges = {}
    if len(panes) > 1:
        if active["cols"] < cols:
            edges["left"] = active["left"] == 0
            edges["right"] = active["left"] + active["cols"] >= cols
        if active["rows"] < rows:
            edges["top"] = active["top"] == 0
            edges["bottom"] = active["top"] + active["rows"] >= rows
    border = ";".join(f"border-{side}-color:{BLUE if edges.get(side) else BG}"
                      for side in ("left", "right", "top", "bottom"))
    splits = []
    for p in panes:
        if p["left"] > 0:  # the gap column left of this pane
            x = PAD_X + (p["left"] - 1) * CW + CW / 2
            splits.append(f'<div class="split" style="left:{x:.2f}px;top:{PAD_Y + p["top"] * LH:.2f}px;'
                          f'width:1.5px;height:{p["rows"] * LH:.2f}px"></div>')
        if p["top"] > 0:
            y = PAD_Y + (p["top"] - 1) * LH + LH / 2
            splits.append(f'<div class="split" style="left:{PAD_X + p["left"] * CW:.2f}px;top:{y:.2f}px;'
                          f'height:1.5px;width:{p["cols"] * CW:.2f}px"></div>')
    page = f"""<!doctype html><meta charset="utf-8"><style>
html,body{{margin:0;background:{BG}}}
.win{{position:relative;width:{W - 2 * EDGE_X:.2f}px;height:{H - 2 * EDGE_Y:.2f}px;background:{BG};
  border-style:solid;border-width:{EDGE_Y:.2f}px {EDGE_X:.2f}px;{border};
  font-family:"JetBrainsMono Nerd Font","JetBrains Mono",monospace;font-size:{FONT_PX}px;
  font-variant-ligatures:none;font-feature-settings:"liga" 0,"calt" 0}}
.pane{{position:absolute;overflow:hidden}}
.pane.dim{{filter:brightness(.7)}}
.row{{position:absolute;left:0;height:{LH}px;width:100%}}
.row span{{position:absolute;top:0;height:{LH}px;line-height:{LH}px;white-space:pre}}
.row i{{display:inline-block;font-style:inherit;text-align:center;overflow:visible}}
.row i.quad{{display:inline-grid;grid-template:1fr 1fr/1fr 1fr;height:{LH}px;vertical-align:top}}
.row i.quad b{{display:block}}
.row i.box{{transform:scaleY(1.25)}}
.split{{position:absolute;background:{BLUE}}}
</style><div class="win">{"".join(splits)}{"".join(pane_html(p, subs) for p in panes)}</div>"""
    page_path = OUT / f"{name}.html"
    page_path.write_text(page)
    png = OUT / f"{name}.png"
    subprocess.run([CHROME, "--headless=new", "--disable-gpu", "--hide-scrollbars",
                    f"--screenshot={png}", f"--window-size={int(W + 0.99)},{int(H + 0.99)}",
                    "--force-device-scale-factor=2", f"file://{page_path}"],
                   check=True, capture_output=True)
    print(f"{name}: {cols}x{rows} -> {png.name}")


def user_subs():
    """Your user name, wherever it shows (a path, ls -l), becomes one as long;
    a longer word starting with it, such as a GitHub handle, stays."""
    user = os.environ.get("USER", "")
    if not user:
        return []
    mock = ("alex.morgan" * 4)[:len(user)]
    return [[re.escape(user) + r"(?![\w])", mock]]


def main():
    subs_file = HERE / "subs.json"
    subs = user_subs() + (json.loads(subs_file.read_text()) if subs_file.exists() else [])
    for name in sys.argv[1:]:
        render(name, subs)


if __name__ == "__main__":
    main()
