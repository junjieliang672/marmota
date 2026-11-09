#!/usr/bin/env python3
"""Saves what every pane in a WezTerm tab shows, colors included.

    capture.py NAME PANE_ID      the tab PANE_ID is in -> out/NAME.json
"""
import json
import subprocess
import sys
from pathlib import Path

OUT = Path(__file__).resolve().parent / "out"


def cli(*args):
    return subprocess.run(["wezterm", "cli", *args], check=True, capture_output=True, text=True).stdout


def main():
    name, pane_id = sys.argv[1], int(sys.argv[2])
    panes = json.loads(cli("list", "--format", "json"))
    tab = next(p["tab_id"] for p in panes if p["pane_id"] == pane_id)
    shot = []
    for p in panes:
        if p["tab_id"] != tab:
            continue
        text = cli("get-text", "--escapes", "--pane-id", str(p["pane_id"]))
        shot.append({
            "pane_id": p["pane_id"], "left": p["left_col"], "top": p["top_row"],
            "cols": p["size"]["cols"], "rows": p["size"]["rows"], "active": p["is_active"],
            "text": text,
        })
    OUT.mkdir(exist_ok=True)
    (OUT / f"{name}.json").write_text(json.dumps(shot))
    print(f"{name}: {len(shot)} pane(s)")


if __name__ == "__main__":
    main()
