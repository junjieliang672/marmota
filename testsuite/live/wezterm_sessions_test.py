#!/usr/bin/env python3
"""Live WezTerm test for Claude sessions in superfile.

Runs a real spf binary inside the WezTerm you're using, but in a workspace of
its own ("spf-live-test") that your window never shows. A fake `claude` on
PATH registers sessions and writes transcripts, so no real Claude session is
started. Checks:

  1. The sidebar shows Pinned and Recent; sessions aren't listed there.
  2. `>claude` opens the first agent beside Marmota.
  3. A second agent (`>claude rc`) comes in beside Marmota with the
     keyboard, and the first is parked in the hidden "superfile-agents"
     workspace, not in a visible tab.
  4. `!` gives the dock's Sessions box the keyboard without switching;
     enter on the other session brings it in beside Marmota with the
     keyboard, and the one that was shown is parked in the hidden workspace.
  5. A new finished turn in the hidden session shows as unread, and `!`
     brings it in.
  6. `!` and enter on the other session switch back.
  7. In the Sessions box `x` asks, then stops the selected session and
     closes its pane; the session leaves the box; Esc gives the keyboard
     back to the files.
  8. `m` mentions the item under the cursor in the Ask draft; `!` then `n`
     opens the dock's Ask box on a new session; sending starts it in the
     background with the question and the context block, and a follow-up
     sent at once goes to that session. The session beside Marmota, and
     the keyboard, stay where they are.
  9. Stopped from the Sessions box, the folder's session leaves it, and
     `!` `n` starts a new session in the background with the next question.
 10. The Ask box, still open, types a request into that session's prompt
     once it is listed; up and down there step through earlier questions,
     not sessions.
 11. The mouse wheel scrolls the file preview under the pointer, down and
     sideways, and does nothing over the file panel.

It never focuses a pane, so your active workspace and window don't change.
Everything it starts is killed at the end.

Usage: python3 testsuite/live/wezterm_sessions_test.py --binary ./bin/y
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
from typing import Any, Callable

TEST_WORKSPACE = "spf-live-test"
HIDDEN_WORKSPACE = "superfile-agents"
HERE = Path(__file__).resolve().parent


def wezterm(*args: str) -> str:
    return subprocess.run(["wezterm", "cli", *args], check=True, capture_output=True, text=True).stdout


def panes() -> list[dict[str, Any]]:
    return json.loads(wezterm("list", "--format", "json"))


def client_state() -> tuple[str, int]:
    clients = json.loads(wezterm("list-clients", "--format", "json"))
    return (clients[0]["workspace"], clients[0]["focused_pane_id"]) if clients else ("", -1)


def screen(pane_id: int) -> str:
    return wezterm("get-text", "--pane-id", str(pane_id))


# A row of the dock's Sessions box, at the screen's left edge: the bar that
# marks the selected session, the number, the state glyph, the agent (cc for
# Claude Code, which the fake claude stands for), the title.
DOCK_ROW = re.compile(r"^│([▌ ]) (\d+) \S cc (.+?)(?:\s{2,}|│)")


def dock_rows(pane_id: int) -> list[tuple[str, str, bool]]:
    """The Sessions box's rows, as (number, title, selected)."""
    rows = []
    in_box = False
    for line in screen(pane_id).splitlines():
        if line.startswith("╭─ ✳ Sessions"):
            in_box = True
        elif in_box and line.startswith("╰"):
            break
        elif in_box and (m := DOCK_ROW.search(line)):
            rows.append((m.group(2), m.group(3), m.group(1) == "▌"))
    return rows


def in_sessions_box(pane_id: int) -> bool:
    """Whether the Sessions box has the keyboard: the status line says how
    to leave it."""
    lines = screen(pane_id).splitlines()
    return bool(lines) and "!/esc:files" in lines[-1]


def selected_session(pane_id: int) -> str:
    """The title of the Sessions box's selected row, or ""."""
    return next((title for _, title, selected in dock_rows(pane_id) if selected), "")


def select_session(pane_id: int, name: str) -> None:
    """Puts the Sessions box's cursor on the session called name, by its
    number: [n]g, as in the file panel."""
    n = wait_for(f"{name} in the Sessions box",
                 lambda: next((number for number, title, _ in dock_rows(pane_id) if name in title), None))
    # One key at a time, as typed: sent together they arrive as one key.
    for key in n + "g":
        type_keys(pane_id, key)
        time.sleep(0.1)
    wait_for(f"the cursor on {name}", lambda: name in selected_session(pane_id))


def type_keys(pane_id: int, text: str) -> None:
    wezterm("send-text", "--pane-id", str(pane_id), "--no-paste", text)


def wait_for(what: str, check: Callable[[], Any], timeout: float = 15.0) -> Any:
    deadline = time.time() + timeout
    while time.time() < deadline:
        result = check()
        if result:
            return result
        time.sleep(0.3)
    raise AssertionError(f"timed out waiting for: {what}")


def superfile_knows_sessions(iso: Path, registry: Path, count: int) -> bool:
    """True once superfile has listed every registered fake session: it notes
    each session in its seen file the first time it lists it."""
    try:
        known = json.loads((iso / "state" / "marmota" / "claude_sessions_seen.json").read_text())
    except (OSError, ValueError):
        return False
    ids = [json.loads(f.read_text())["sessionId"] for f in registry.glob("*.json")]
    return len(ids) >= count and all(i in known for i in ids)


def pane_by_id(pane_id: int) -> dict[str, Any] | None:
    return next((p for p in panes() if p["pane_id"] == pane_id), None)


def is_test_pane(pane: dict[str, Any], work: Path) -> bool:
    """True only for panes running in this test's own temporary folder.

    Your real superfile parks its agent panes in the same hidden workspace,
    so the workspace alone never marks a pane as the test's.
    """
    return str(work) in pane.get("cwd", "") or pane.get("workspace") == TEST_WORKSPACE


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", required=True)
    parser.add_argument("--keep", action="store_true", help="leave the panes running for inspection")
    args = parser.parse_args()
    if not os.environ.get("WEZTERM_PANE"):
        print("SKIP: not running inside WezTerm")
        return 0

    binary = str(Path(args.binary).resolve())
    work = Path(tempfile.mkdtemp(prefix="spf-live-", dir="/private/tmp"))
    project = work / "project"
    (project / "assets").mkdir(parents=True)
    (project / "notes.md").write_text("hello\n")
    # Longer than the preview shows, down and sideways.
    (project / "long.md").write_text("row 001 " + "-" * 200 + " far right\n"
                                     + "".join(f"row {i:03d}\n" for i in range(2, 121)))
    registry = work / "registry"
    claude_home = work / "claude-home"
    iso = work / "iso"
    # One pinned folder and one recent file, so the sidebar shows both
    # sections instead of "None".
    data = iso / "data" / "marmota"
    data.mkdir(parents=True)
    (data / "pinned.json").write_text(json.dumps([{"location": str(project / "assets"), "name": "assets"}]))
    (data / "recent_edits.json").write_text(json.dumps(
        [{"location": str(project / "notes.md"), "name": "notes.md", "edited_at": int(time.time())}]))

    env_args = [
        "env",
        f"PATH={HERE / 'fakes'}:{os.environ['PATH']}",
        f"FAKE_CLAUDE_REG={registry}",
        f"CLAUDE_CONFIG_DIR={claude_home}",
        # A Codex home with no server: the computer's own Codex sessions
        # stay out of the test's list.
        f"CODEX_HOME={work / 'codex-home'}",
        f"XDG_DATA_HOME={iso / 'data'}",
        f"XDG_STATE_HOME={iso / 'state'}",
        f"XDG_CACHE_HOME={iso / 'cache'}",
        "TERM=xterm-256color",
    ]
    before_workspace, before_focus = client_state()
    started: list[int] = []
    try:
        # Sessions mode wants room: the window superfile shares with a session.
        spf = int(subprocess.run(
            ["wezterm", "--config", "initial_cols=200", "--config", "initial_rows=50", "cli", "spawn",
             "--new-window", "--workspace", TEST_WORKSPACE, "--cwd", str(project), "--", *env_args, binary],
            check=True, capture_output=True, text=True).stdout.strip())
        started.append(spf)
        spf_tab = pane_by_id(spf)["tab_id"]
        # A fresh data folder shows the first-run welcome; any key dismisses it.
        wait_for("superfile to start", lambda: "Thanks for using Marmota" in screen(spf)
                 or "K:ask" in screen(spf))
        if "Thanks for using Marmota" in screen(spf):
            type_keys(spf, "\x1b")
        wait_for("the main screen", lambda: "K:ask" in screen(spf))
        # Hide the dock: the steps below bring it back when they need it.
        type_keys(spf, "F")
        print("ok   superfile is running in the test workspace")
        # No sidebar column: s opens it as a popup over the panels.
        type_keys(spf, "s")
        wait_for("the sidebar popup", lambda: "Pinned" in screen(spf) and "Recent" in screen(spf)
                 and "path /" in screen(spf))
        type_keys(spf, "\x1b")
        wait_for("the popup to close", lambda: "path /" not in screen(spf))
        print("ok   s opens the sidebar as a popup with Pinned, Recent and the picked path; esc closes it")

        # 1. first agent opens beside Marmota
        type_keys(spf, ">")
        time.sleep(0.5)
        type_keys(spf, "claude\r")
        first = wait_for("first agent pane", lambda: next(
            (p for p in panes() if p["tab_id"] == spf_tab and p["pane_id"] != spf and is_test_pane(p, work)), None))
        started.append(first["pane_id"])
        print(f"ok   first agent opened beside Marmota (pane {first['pane_id']})")

        # 2. a second agent comes in beside Marmota; the first is parked
        type_keys(spf, ">")
        time.sleep(0.5)
        type_keys(spf, "claude rc\r")

        def second_pane() -> dict[str, Any] | None:
            known = set(started)
            return next((p for p in panes() if p["pane_id"] not in known
                         and p["tab_id"] == spf_tab and is_test_pane(p, work)), None)

        second = wait_for("second agent beside Marmota", second_pane)
        started.append(second["pane_id"])
        wait_for("first session parked",
                 lambda: (pane_by_id(first["pane_id"]) or {}).get("workspace") == HIDDEN_WORKSPACE)
        assert (pane_by_id(second["pane_id"]) or {}).get("is_active"), "the new session didn't get the keyboard"
        print(f"ok   second agent came in beside Marmota (pane {second['pane_id']}); the first is parked "
              f"in '{HIDDEN_WORKSPACE}'")

        # 4. ! gives the Sessions box the keyboard; enter on the other session
        # swaps it in.
        wait_for("superfile to list both sessions", lambda: superfile_knows_sessions(iso, registry, 2))
        type_keys(spf, "!")
        wait_for("the Sessions box", lambda: in_sessions_box(spf))
        titles = [title for _, title, _ in dock_rows(spf)]
        assert any("local1" in t for t in titles) and any("remote2" in t for t in titles), \
            f"the box doesn't show both sessions: {titles}"
        assert (pane_by_id(second["pane_id"]) or {}).get("tab_id") == spf_tab, "! switched before enter"
        select_session(spf, "local1")
        type_keys(spf, "\r")
        wait_for("enter to show the first session beside Marmota",
                 lambda: (pane_by_id(first["pane_id"]) or {}).get("tab_id") == spf_tab)
        parked = wait_for("rc session parked",
                          lambda: (pane_by_id(second["pane_id"]) or {}).get("workspace") == HIDDEN_WORKSPACE)
        assert parked
        assert (pane_by_id(first["pane_id"]) or {}).get("is_active"), "enter didn't put the keyboard in the session"
        type_keys(spf, "\x1b")  # esc: back to the files
        wait_for("the files again", lambda: not in_sessions_box(spf))
        print("ok   ! listed both sessions in the Sessions box; enter on the other switched to it, focused "
              "it and parked the rc one in the hidden workspace")

        # 5. a new turn in the now-hidden rc session is unread, and ! jumps to it
        second_id = next(json.loads(f.read_text())["sessionId"] for f in registry.glob("*.json")
                         if json.loads(f.read_text())["name"] == "remote2")
        transcript = Path((registry / f"{second_id}.transcript").read_text().strip())
        time.sleep(1.2)
        now = time.strftime("%Y-%m-%dT%H:%M:%S.999Z", time.gmtime())
        with transcript.open("a") as f:
            f.write(json.dumps({"type": "assistant", "timestamp": now, "message": {
                "role": "assistant", "content": [{"type": "text", "text": "Done with the second task."}]}}) + "\n")
            f.write(json.dumps({"type": "system", "subtype": "turn_duration", "timestamp": now}) + "\n")
        wait_for("unread marker", lambda: "1 unread" in screen(spf))
        print("ok   the hidden session's new reply shows as unread in the status line")

        # ! puts the keyboard in the dock's Sessions box, on the session the
        # toast is about; enter brings it in beside Marmota.
        type_keys(spf, "!")
        wait_for("! to select the unread session in the dock",
                 lambda: "!/esc:files" in screen(spf).splitlines()[-1])
        type_keys(spf, "\r")
        wait_for("enter to bring the unread session in",
                 lambda: (pane_by_id(second["pane_id"]) or {}).get("tab_id") == spf_tab)
        # "!:unread" is a key hint; the count reads "● 1 unread (!)".
        wait_for("unread cleared", lambda: "unread (!)" not in screen(spf).splitlines()[-1])
        print("ok   ! selected the unread session, enter showed it, and the marker cleared")

        # 6. ! and enter on the other session switch back
        type_keys(spf, "\x1b")  # the box still has the keyboard after enter
        wait_for("the files again", lambda: not in_sessions_box(spf))
        type_keys(spf, "!")
        wait_for("the Sessions box", lambda: in_sessions_box(spf))
        select_session(spf, "local1")
        type_keys(spf, "\r")
        wait_for("enter to show the first session again",
                 lambda: (pane_by_id(first["pane_id"]) or {}).get("tab_id") == spf_tab)
        print("ok   ! and enter on the other session switched back")

        # 7. x stops the selected session: remote2
        if not in_sessions_box(spf):
            type_keys(spf, "!")
            wait_for("the Sessions box", lambda: in_sessions_box(spf))
        select_session(spf, "remote2")
        type_keys(spf, "x")
        wait_for("the stop question", lambda: "Stop" in screen(spf))
        type_keys(spf, "\r")
        wait_for("the stopped session's pane to close", lambda: pane_by_id(second["pane_id"]) is None)
        wait_for("remote2 to leave the box", lambda: not any("remote2" in t for _, t, _ in dock_rows(spf)))
        print("ok   x asked, then stopped remote2 and closed its pane; it left the Sessions box")
        type_keys(spf, "\x1b")
        wait_for("the files", lambda: not in_sessions_box(spf))
        print("ok   Esc gave the keyboard back to the files")

        # 8. the Ask box in the dock: m mentions the item under the cursor,
        # ! then n writes to a new session, which starts in the background;
        # the session beside Marmota, and the keyboard, stay
        type_keys(spf, "m")
        time.sleep(0.3)
        type_keys(spf, "!")
        wait_for("the Sessions box", lambda: "!/esc:files" in screen(spf).splitlines()[-1])
        type_keys(spf, "n")
        wait_for("the Ask box on a new session", lambda: "Ask → new Claude Code session in project" in screen(spf))
        wait_for("the item under the cursor in the question", lambda: "@assets/" in screen(spf))
        type_keys(spf, "what is in here?")
        time.sleep(0.3)
        type_keys(spf, "\r")
        # a follow-up sent at once waits for that session: no other starts
        time.sleep(0.3)
        type_keys(spf, "and in src?")
        time.sleep(0.3)
        type_keys(spf, "\r")

        def asked_session(question: str) -> str | None:
            for f in registry.glob("*.transcript"):
                if question in Path(f.read_text().strip()).read_text():
                    return f.stem
            return None

        def hidden_new_pane() -> dict[str, Any] | None:
            return next((p for p in panes() if p["pane_id"] not in set(started)
                         and p.get("workspace") == HIDDEN_WORKSPACE and is_test_pane(p, work)), None)

        def stays_put(why: str) -> None:
            assert (pane_by_id(first["pane_id"]) or {}).get("tab_id") == spf_tab, \
                f"{why}: the session beside Marmota changed"
            assert (pane_by_id(spf) or {}).get("is_active"), f"{why}: the keyboard left superfile"
            assert "enter send" in screen(spf), f"{why}: the Ask bar lost the keyboard"

        new_id = wait_for("the new session", lambda: asked_session("what is in here?"))
        transcript = Path((registry / f"{new_id}.transcript").read_text().strip()).read_text()
        assert "@assets/ what is in here?\\n\\n<superfile-context>\\ncwd:" in transcript, transcript
        asked_pane = wait_for("the new session in the hidden workspace", hidden_new_pane)
        started.append(asked_pane["pane_id"])
        stays_put("sending to a new session")
        wait_for("superfile to list it", lambda: superfile_knows_sessions(iso, registry, 2))
        wait_for("the follow-up typed into it", lambda: "and in src?" in screen(asked_pane["pane_id"]))
        time.sleep(1)
        assert hidden_new_pane() is None, "the follow-up started another session"
        stays_put("sending a follow-up")
        print(f"ok   ! n in the dock started a session in the background (pane {asked_pane['pane_id']}) with "
              "the question, the item m mentioned and the context block, and the follow-up sent at once went "
              "to it; local1 stayed beside Marmota, and the keyboard in the Ask box")

        # 9. stopped, the folder's session leaves the list: ! n starts a new
        # one, in the background too
        type_keys(spf, "\x1b")  # esc closes the Ask box, back to the Sessions box
        wait_for("the Ask box closed", lambda: "Ask → " not in screen(spf))
        if not in_sessions_box(spf):
            type_keys(spf, "!")
            wait_for("the Sessions box", lambda: in_sessions_box(spf))
        # the session the Ask box started: the one that isn't local1
        asked_title = wait_for("the asked session in the box", lambda: next(
            (t for _, t, _ in dock_rows(spf) if "local1" not in t), None))
        select_session(spf, asked_title.split()[0])
        listed = len(dock_rows(spf))
        type_keys(spf, "x")
        wait_for("the stop question", lambda: "Stop" in screen(spf))
        type_keys(spf, "\r")
        wait_for("its pane to close", lambda: pane_by_id(asked_pane["pane_id"]) is None)
        wait_for("it to leave the box", lambda: len(dock_rows(spf)) == listed - 1)
        type_keys(spf, "n")
        wait_for("the Ask box on a new session", lambda: "Ask → new Claude Code session in project" in screen(spf))
        type_keys(spf, "\x15")  # ctrl+u: a question of its own
        type_keys(spf, "now write the notes")
        time.sleep(0.3)
        type_keys(spf, "\r")
        noted_id = wait_for("a session with the request", lambda: asked_session("now write the notes"))
        assert noted_id != new_id, "the stopped session was resumed, not a new one started"
        noted = wait_for("the new session in the hidden workspace", hidden_new_pane)
        started.append(noted["pane_id"])
        stays_put("sending to another new session")
        print(f"ok   stopped, the folder's session left the list, and ! n started a new session in the "
              f"background (pane {noted['pane_id']}) with the request")

        # 10. the Ask bar types into a running session; up and down there
        # step through earlier questions, without choosing another session
        wait_for("superfile to list the new session as running",
                 lambda: superfile_knows_sessions(iso, registry, 2))
        # The Ask box stays open, and writes to the session it started once
        # it is listed.
        wait_for("the Ask box on a running session", lambda: "typed into its pane" in screen(spf))
        chosen = next(line for line in screen(spf).splitlines() if "Ask → " in line)
        type_keys(spf, "\x1b[A")  # up: the question sent last
        wait_for("the last question back in the bar", lambda: "› now write the notes" in screen(spf))
        type_keys(spf, "\x1b[B")  # down: back to what was there, nothing
        wait_for("the bar empty again", lambda: "Write to this session, or describe" in screen(spf))
        assert chosen in screen(spf).splitlines(), "up and down in the Ask bar chose another session"
        type_keys(spf, "and add a test")
        time.sleep(0.3)
        type_keys(spf, "\r")
        wait_for("the request typed into the session", lambda: "and add a test" in screen(noted["pane_id"]))
        stays_put("typing into a running session")
        print("ok   the Ask bar typed the request into the running session's prompt; up and down stepped "
              "through earlier questions and kept the session it writes to; the keyboard stayed in the bar")

        # 11. the mouse wheel scrolls the file preview under the pointer, down
        # and sideways, and nothing else: here as WezTerm sends it (SGR: 64 up,
        # 65 down, 66 left, 67 right), at a 1-based column and row
        type_keys(spf, "\x1b")  # the Ask box: back to where it opened
        time.sleep(0.3)
        if in_sessions_box(spf):
            type_keys(spf, "\x1b")  # the files
        wait_for("the files", lambda: not in_sessions_box(spf) and "Ask → " not in screen(spf))
        number = wait_for("long.md in the file panel", lambda: next(
            (m.group(1) for line in screen(spf).splitlines() if (m := re.search(r"│\S?\s*(\d+) \S long\.md", line))),
            None))
        for key in number + "g":  # [n]g moves the cursor; never enter on a file
            type_keys(spf, key)
            time.sleep(0.1)
        if "Preview" not in screen(spf):
            type_keys(spf, "\x06")  # ctrl+f opens it
        wait_for("long.md in the preview", lambda: "row 001" in screen(spf))
        top = screen(spf).splitlines()[0]
        column = top.index("Preview") + 2

        def wheel(button: int, col: int) -> None:
            type_keys(spf, f"\x1b[<{button};{col};6M")
            time.sleep(0.3)

        wheel(65, column)
        wait_for("the preview a turn down", lambda: "row 004" in screen(spf) and "row 001" not in screen(spf))
        wheel(64, column)
        wait_for("the preview back at its top", lambda: "row 001" in screen(spf))
        for _ in range(60):
            type_keys(spf, f"\x1b[<67;{column};6M")  # right, as a trackpad sends it: many at once
        wait_for("the end of the long line", lambda: "far right" in screen(spf) and "row 001" not in screen(spf))
        for _ in range(60):
            type_keys(spf, f"\x1b[<66;{column};6M")
        wait_for("the preview back at its left edge", lambda: "row 001 ---" in screen(spf))
        def panels() -> list[str]:
            """The sidebar, the file panel and the preview, without the top rows
            a toast may show over, and without the dock under them, whose MAC
            readings change every 2 seconds on their own."""
            lines = screen(spf).splitlines()
            end = next((i for i, line in enumerate(lines) if line.startswith("╰")), len(lines) - 3)
            return lines[5:end]

        before = panels()
        for button in (65, 64, 66, 67):
            wheel(button, 30)  # over the file panel
        wheel(67, 900)  # outside superfile's screen: WezTerm sends sideways scrolls to the focused pane
        time.sleep(0.5)
        assert panels() == before, "the wheel over the file panel, or from outside the screen, changed something"
        print("ok   the wheel scrolled the file preview under the pointer, down and sideways; over the file panel, "
              "and from outside the screen, it did nothing")

        after_workspace, after_focus = client_state()
        assert (after_workspace, after_focus) == (before_workspace, before_focus), \
            f"the test changed your window: {before_workspace}/{before_focus} -> {after_workspace}/{after_focus}"
        print("ok   your active workspace and focused pane are unchanged")
        print("PASS")
        return 0
    except AssertionError as e:
        print(f"FAIL {e}")
        if started:
            print("---- superfile screen ----")
            print(screen(started[0]))
        return 1
    finally:
        if not args.keep:
            for pane in panes():
                if is_test_pane(pane, work):
                    subprocess.run(["wezterm", "cli", "kill-pane", "--pane-id", str(pane["pane_id"])],
                                   check=False, capture_output=True)
            shutil.rmtree(work, ignore_errors=True)


if __name__ == "__main__":
    sys.exit(main())
