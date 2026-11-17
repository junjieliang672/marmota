#!/usr/bin/env python3
"""Live WezTerm test for sessions in tmux, and for upgrading to them.

Runs real Marmota binaries inside the WezTerm you're using, in a workspace of
its own ("spf-tmux-test") that your window never shows, with a fake `claude`
on PATH and a tmux server of its own (MARMOTA_TMUX_SOCKET). Checks:

  1. The old Marmota (--old, built from before sessions ran in tmux) opens
     two sessions, one beside it and one parked in the hidden workspace; a
     third is started by hand in a pane of its own.
  2. The old Marmota quits and the new one (--binary) starts in another
     pane: the Sessions box lists the same three sessions. The parked, idle
     one moves into tmux: its WezTerm pane closes and it runs on in a tmux
     window; the one beside the old Marmota stays where it was.
  3. enter on the moved session shows it beside the new Marmota: one pane
     that attaches to tmux.
  4. The Ask box starts a new session in a tmux window, with no WezTerm pane;
     a follow-up is typed into it.
  5. x stops it: its window closes and it leaves the Sessions box.
  6. WezTerm loses Marmota and the pane beside it: a new Marmota lists every
     session still running, and enter shows a tmux one beside it again.
  7. `y session start --on` starts a session on "another computer" (this
     one, through a command, with a tmux server of its own): it shows in the
     Sessions box with that computer's name, `y session send` and `read`
     reach it, enter shows it beside Marmota, and x stops it.

It never focuses a pane, so your active workspace and window don't change.
Everything it starts is killed at the end, its tmux server too.

Usage: python3 testsuite/live/tmux_sessions_test.py --binary /tmp/marmota-new/y --old /tmp/marmota-old/bin/y
"""

from __future__ import annotations

import argparse
import json
import os
import re
import shlex
import shutil
import subprocess
import sys
import tempfile
import time
from pathlib import Path
from typing import Any

from wezterm_sessions_test import (HERE, client_state, dock_rows, in_sessions_box, pane_by_id, panes, screen,
                                   select_session, superfile_knows_sessions, type_keys, wait_for)

TEST_WORKSPACE = "spf-tmux-test"
HIDDEN_WORKSPACE = "superfile-agents"


def row_of_host(text: str, host: str, title: str) -> str | None:
    """The Sessions box's row of a session on host. Its name is "host · title"
    until it is renamed; a narrow box may cut the title short."""
    for line in text.splitlines():
        box = line.split("││", 1)[0]  # the Sessions box's part: the Ask box is on the same lines
        if not box.startswith("│") or " cc " not in box:
            continue  # not a session's row: the preview's title says the name too
        if host + " · " in box and (title in box or title[:3] in box.split(host + " · ", 1)[1]):
            return box
    return None


def title_after(row: str, host: str) -> str:
    """The title in a row whose session's name is "host · title"."""
    return row.split(host + " · ", 1)[1].split()[0]


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", required=True)
    parser.add_argument("--old", required=True)
    parser.add_argument("--keep", action="store_true")
    args = parser.parse_args()
    if not os.environ.get("WEZTERM_PANE"):
        print("SKIP: not running inside WezTerm")
        return 0

    new, old = str(Path(args.binary).resolve()), str(Path(args.old).resolve())
    work = Path(tempfile.mkdtemp(prefix="spf-tmux-", dir="/private/tmp"))
    project = work / "project"
    (project / "assets").mkdir(parents=True)
    (project / "notes.md").write_text("hello\n")
    registry, claude_home, iso = work / "registry", work / "claude-home", work / "iso"
    (work / "home").mkdir()
    socket = f"marmota-live-{os.getpid()}"

    def tmux(*a: str) -> str:
        return subprocess.run(["tmux", "-L", socket, *a], capture_output=True, text=True).stdout

    def windows() -> dict[str, str]:
        """The test's tmux windows that hold a session, name → pane."""
        out = tmux("list-panes", "-s", "-t", "marmota", "-F", "#{window_name} #{pane_id}")
        return {name: pane for name, pane in (line.split() for line in out.splitlines())
                if name.startswith(("cc-", "co-"))}

    env = {
        "PATH": f"{HERE / 'fakes'}:{os.environ['PATH']}",
        "FAKE_CLAUDE_REG": str(registry),
        "CLAUDE_CONFIG_DIR": str(claude_home),
        "CODEX_HOME": str(work / "codex-home"),
        "XDG_DATA_HOME": str(iso / "data"),
        "XDG_STATE_HOME": str(iso / "state"),
        "XDG_CACHE_HOME": str(iso / "cache"),
        "MARMOTA_TMUX_SOCKET": socket,
        # It never has your focus: it mustn't hold your input method in ASCII.
        # A home of its own keeps the old Marmota, which knows no
        # MARMOTA_NO_IME_HOLD, from it too: the hold is a file under ~.
        "MARMOTA_NO_IME_HOLD": "1",
        "HOME": str(work / "home"),
        "TERM": "xterm-256color",
    }
    # Run from a session reached over ssh, or in tmux, Marmota would start
    # its tmux servers through launchd, as on a Mac it was reached at.
    env_args = ["env", "-u", "SSH_CONNECTION", "-u", "TMUX", "-u", "TMUX_PANE",
                *[f"{k}={v}" for k, v in env.items()]]

    def spawn(*command: str) -> int:
        return int(subprocess.run(
            ["wezterm", "--config", "initial_cols=200", "--config", "initial_rows=50", "cli", "spawn",
             "--new-window", "--workspace", TEST_WORKSPACE, "--cwd", str(project), "--", *env_args, *command],
            check=True, capture_output=True, text=True).stdout.strip())

    def is_test_pane(p: dict[str, Any]) -> bool:
        return str(work) in p.get("cwd", "") or p.get("workspace") == TEST_WORKSPACE

    def main_screen(spf: int) -> None:
        wait_for("Marmota to start", lambda: "Thanks for using Marmota" in screen(spf) or "K:ask" in screen(spf))
        if "Thanks for using Marmota" in screen(spf):
            type_keys(spf, "\x1b")
        wait_for("the main screen", lambda: "K:ask" in screen(spf))

    def listed(spf: int) -> set[str]:
        """The titles in the Sessions box, read with the keyboard in it."""
        if not in_sessions_box(spf):
            type_keys(spf, "!")
            try:
                wait_for("the Sessions box", lambda: in_sessions_box(spf), timeout=8)
            except AssertionError:
                # A Marmota that has just started may drop the first key; one
                # that hangs fails this second time.
                type_keys(spf, "!")
                wait_for("the Sessions box", lambda: in_sessions_box(spf))
        titles = {title.split()[0] for _, title, _ in dock_rows(spf)}
        type_keys(spf, "\x1b")
        wait_for("the files", lambda: not in_sessions_box(spf))
        return titles

    before_workspace, before_focus = client_state()
    spf = 0
    try:
        # 1. the old Marmota, in a shell that starts the new one after it
        script = f"{shlex.quote(old)}; sleep 600"
        spf = spawn("/bin/sh", "-c", script)
        spf_tab = pane_by_id(spf)["tab_id"]
        main_screen(spf)
        type_keys(spf, "F")
        for command in ("claude\r", "claude rc\r"):
            type_keys(spf, ">")
            time.sleep(0.5)
            type_keys(spf, command)
            time.sleep(2)
        wait_for("two sessions", lambda: superfile_knows_sessions(iso, registry, 2))
        parked = wait_for("one parked in the hidden workspace", lambda: next(
            (p for p in panes() if p.get("workspace") == HIDDEN_WORKSPACE and is_test_pane(p)), None))
        beside = wait_for("one beside Marmota", lambda: next(
            (p for p in panes() if p["tab_id"] == spf_tab and p["pane_id"] != spf and is_test_pane(p)), None))
        by_hand = spawn("claude")
        wait_for("the one started by hand", lambda: superfile_knows_sessions(iso, registry, 3))
        old_titles = wait_for("the old Marmota to list three", lambda: (lambda t: t if len(t) == 3 else None)(listed(spf)))
        assert not windows(), "the old Marmota started nothing in tmux"
        print(f"ok   the old Marmota lists {sorted(old_titles)}: one beside it, one parked, one started by hand")

        # 2. quit; the new Marmota starts in a pane of its own
        type_keys(spf, "q")
        wait_for("the old Marmota to quit", lambda: "K:ask" not in screen(spf))
        old_tab = spf_tab
        spf = spawn(new)
        spf_tab = pane_by_id(spf)["tab_id"]
        main_screen(spf)
        type_keys(spf, "F")
        moved = wait_for("the parked session to move into tmux", lambda: next(iter(windows().values()), None)
                         if pane_by_id(parked["pane_id"]) is None else None, timeout=30)
        new_titles = wait_for("the new Marmota to list the same three",
                              lambda: (lambda t: t if t == old_titles else None)(listed(spf)), timeout=20)
        assert pane_by_id(beside["pane_id"])["tab_id"] == old_tab, "the session beside the old Marmota moved"
        assert pane_by_id(by_hand) is not None, "the session started by hand was touched"
        print(f"ok   the new Marmota lists the same sessions {sorted(new_titles)}; the parked one runs on in "
              f"tmux (pane {moved}); the one beside Marmota and the one started by hand are untouched")

        # 3. enter on the moved one shows it beside Marmota
        moved_title = "local1"  # the fake names sessions in the order they start: >claude was the first
        type_keys(spf, "!")
        wait_for("the Sessions box", lambda: in_sessions_box(spf))
        select_session(spf, moved_title)
        type_keys(spf, "\r")
        viewer = wait_for("a pane beside Marmota that attaches to tmux", lambda: next(
            (p for p in panes() if p["tab_id"] == spf_tab and p["pane_id"] != spf), None))
        wait_for("it to draw the moved session", lambda: screen(viewer["pane_id"]).strip())
        type_keys(spf, "\x1b")
        print(f"ok   enter showed the moved session beside Marmota in pane {viewer['pane_id']}, attached to tmux")

        # 4. the Ask box starts a new session in tmux
        if in_sessions_box(spf):
            type_keys(spf, "\x1b")
        type_keys(spf, "!")
        wait_for("the Sessions box", lambda: in_sessions_box(spf))
        type_keys(spf, "n")
        wait_for("the Ask box on a new session", lambda: "Ask → new Claude Code session" in screen(spf))
        known = set(windows())
        panes_before = {p["pane_id"] for p in panes() if is_test_pane(p)}
        type_keys(spf, "what is in here?")
        time.sleep(0.3)
        type_keys(spf, "\r")
        time.sleep(0.3)
        type_keys(spf, "and in src?")
        time.sleep(0.3)
        type_keys(spf, "\r")
        new_window = wait_for("a new tmux window", lambda: next((n for n in windows() if n not in known), None))
        new_pane = windows()[new_window]
        wait_for("the follow-up typed into it", lambda: "and in src?" in tmux("capture-pane", "-p", "-t", new_pane),
                 timeout=30)
        opened = {p["pane_id"] for p in panes() if is_test_pane(p)} - panes_before
        assert not opened, f"a WezTerm pane opened: {opened}"
        print(f"ok   the Ask box started a session in tmux window {new_window}, with no WezTerm pane, "
              "and typed the follow-up into it")

        # 5. x stops it
        type_keys(spf, "\x1b")
        wait_for("the Ask box closed", lambda: "Ask → " not in screen(spf))
        if not in_sessions_box(spf):
            type_keys(spf, "!")
            wait_for("the Sessions box", lambda: in_sessions_box(spf))
        new_title = wait_for("the new session listed", lambda: next(
            (t.split()[0] for _, t, _ in dock_rows(spf) if t.split()[0] not in new_titles), None))
        select_session(spf, new_title)
        type_keys(spf, "x")
        wait_for("the stop question", lambda: "Stop" in screen(spf))
        type_keys(spf, "\r")
        wait_for("its window to close", lambda: new_window not in windows())
        wait_for("it to leave the box", lambda: not any(new_title in t for _, t, _ in dock_rows(spf)))
        type_keys(spf, "\x1b")
        print("ok   x stopped the new session: its tmux window closed and it left the Sessions box")

        # 6. WezTerm loses Marmota and the pane beside it
        for pane in (viewer["pane_id"], spf):
            subprocess.run(["wezterm", "cli", "kill-pane", "--pane-id", str(pane)], capture_output=True)
        time.sleep(1)
        assert moved in windows().values(), "the session in tmux died with WezTerm's panes"
        spf = spawn(new)
        spf_tab = pane_by_id(spf)["tab_id"]
        main_screen(spf)
        type_keys(spf, "F")
        again = wait_for("the restarted Marmota to list them all",
                         lambda: (lambda t: t if t == old_titles else None)(listed(spf)), timeout=20)
        type_keys(spf, "!")
        wait_for("the Sessions box", lambda: in_sessions_box(spf))
        select_session(spf, moved_title)
        type_keys(spf, "\r")
        wait_for("the moved session beside the new Marmota", lambda: next(
            (p for p in panes() if p["tab_id"] == spf_tab and p["pane_id"] != spf), None))
        type_keys(spf, "\x1b")
        print(f"ok   after WezTerm lost Marmota and the pane beside it, a new Marmota listed {sorted(again)} "
              "and showed the tmux session beside it")

        # 7. a session on another computer
        remote_socket = socket + "-remote"
        on = f"env MARMOTA_TMUX_SOCKET={remote_socket} /bin/sh -c"

        def y(*a: str) -> str:
            return subprocess.run([*env_args, new, "session", *a],
                                  check=True, capture_output=True, text=True, cwd=project).stdout

        started_out = y("start", "--on", on, "--cwd", str(project), "--", "from far away")
        remote_id = started_out.split()[1]
        host = started_out.split(" on ")[1].split(" in ")[0]
        listed_json = json.loads(y("list", "--json"))
        assert any(s["id"] == remote_id and s["host"] == host for s in listed_json["sessions"]), listed_json
        y("send", remote_id[:8], "and one more")
        wait_for("the message in the remote session", lambda: "and one more" in subprocess.run(
            ["tmux", "-L", remote_socket, "capture-pane", "-p", "-t", "cc-" + remote_id],
            capture_output=True, text=True).stdout)
        assert "from far away" in y("read", remote_id[:8])
        type_keys(spf, "!")
        wait_for("the Sessions box", lambda: in_sessions_box(spf))
        row = wait_for("the remote session's row", lambda: row_of_host(screen(spf), host, ""))
        remote_title = title_after(row, host)

        def pick_remote() -> None:
            """Puts the Sessions box on the remote row by its number: the Ask box
            says whom it writes to."""
            number = re.search(r"│\S?\s*(\d+) ", row_of(remote_title)).group(1)
            for key in number + "g":
                type_keys(spf, key)
                time.sleep(0.1)
            wait_for("the remote session picked",
                     lambda: (row_of_host(screen(spf), host, remote_title) or "").startswith("│▌"))

        def screen_of(pane_id: int) -> str:
            """A pane's screen, or nothing if it closed since it was listed:
            the viewer of another computer closes as this one opens."""
            try:
                return screen(pane_id)
            except subprocess.CalledProcessError:
                return ""

        def row_of(title: str) -> str:
            return wait_for(f"{title}'s row", lambda: row_of_host(screen(spf), host, title))

        pick_remote()
        type_keys(spf, "\r")
        remote_viewer = wait_for("it beside Marmota", lambda: next(
            (p for p in panes() if p["tab_id"] == spf_tab and p["pane_id"] != spf
             and "and one more" in screen_of(p["pane_id"])), None))
        type_keys(spf, "\x1b")
        wait_for("the files", lambda: not in_sessions_box(spf))
        type_keys(spf, "!")
        wait_for("the Sessions box", lambda: in_sessions_box(spf))
        pick_remote()
        type_keys(spf, "x")
        wait_for("the stop question", lambda: "Stop" in screen(spf))
        type_keys(spf, "\r")
        wait_for("its window to close over there", lambda: "cc-" + remote_id not in subprocess.run(
            ["tmux", "-L", remote_socket, "list-windows", "-a", "-F", "#{window_name}"],
            capture_output=True, text=True).stdout)
        wait_for("the pane beside Marmota to close", lambda: pane_by_id(remote_viewer["pane_id"]) is None)
        type_keys(spf, "\x1b")
        subprocess.run(["tmux", "-L", remote_socket, "kill-server"], capture_output=True)
        print(f"ok   y session start --on started a session on {host}; it showed in the Sessions box with its "
              "computer's name; send and read reached it; enter showed it beside Marmota; x stopped it")

        # 8. a computer that needs a login: its session stays, marked; enter
        # logs in, in a pane below Marmota, and it is reached again
        remote_socket2 = socket + "-remote2"
        marker = work / "logged-in"
        marker.touch()
        gate = (f"[ -f {marker} ] || {{ echo 'Permission denied (live test)' >&2; exit 255; }}; "
                f"export MARMOTA_TMUX_SOCKET={remote_socket2}; eval \"$1\"")
        on2 = f"/bin/sh -c {shlex.quote(gate)} sh"
        out2 = y("start", "--on", on2, "--cwd", str(project), "--", "behind a login")
        host2 = out2.split(" on ")[1].split(" in ")[0]
        remotes_file = iso / "state" / "marmota" / "remotes.json"
        hosts = json.loads(remotes_file.read_text())
        for h in hosts:
            if h["name"] == host2:
                h["login"] = ["touch", str(marker)]
        remotes_file.write_text(json.dumps(hosts))
        row2 = wait_for("its row", lambda: row_of_host(screen(spf), host2, ""))
        title2 = title_after(row2, host2)

        def screen_after(title: str) -> str:
            """The line under a session's row: what it is doing."""
            lines = screen(spf).splitlines()
            for i, line in enumerate(lines):
                box = line.split("││", 1)[0]
                if box.startswith("│") and " cc " in box and host2 + " · " in box and title[:3] in box \
                        and i + 1 < len(lines):
                    return lines[i + 1].split("││", 1)[0]
            return ""

        # The connection drops, and the login has run out.
        marker.unlink()
        clients = subprocess.run(["tmux", "-L", remote_socket2, "list-clients", "-F",
                                  "#{client_name} #{client_control_mode}"], capture_output=True, text=True).stdout
        for line in clients.splitlines():
            name, control = line.rsplit(" ", 1)
            if control == "1":
                subprocess.run(["tmux", "-L", remote_socket2, "detach-client", "-t", name], capture_output=True)
        wait_for("its row to say it needs a login", lambda: "needs login" in screen_after(title2), timeout=30)
        type_keys(spf, "!")
        wait_for("the Sessions box", lambda: in_sessions_box(spf))
        number2 = re.search(r"│\S?\s*(\d+) ", row_of_host(screen(spf), host2, title2)).group(1)
        for key in number2 + "g":
            type_keys(spf, key)
            time.sleep(0.1)
        wait_for("its row picked", lambda: (row_of_host(screen(spf), host2, title2) or "").startswith("│▌"))
        type_keys(spf, "\r")
        wait_for("the login to run", lambda: marker.exists())
        wait_for("it reached again", lambda: "waiting for your next" in screen_after(title2), timeout=40)
        type_keys(spf, "\x1b")
        print(f"ok   {host2} needing a login kept its session in the list, marked; enter logged in in a pane, "
              "and it was reached again")

        # 9. Marmota restarts in its pane: the pane beside it that shows the
        # other computer's session is taken back, not opened twice
        # Between the two, a line the test waits for: the second Marmota can
        # draw before a look at the screen finds the first one gone.
        spf3 = spawn("/bin/sh", "-c", f"{shlex.quote(new)}; echo marmota-quit; sleep 2; {shlex.quote(new)}; sleep 600")
        tab3 = pane_by_id(spf3)["tab_id"]
        main_screen(spf3)
        type_keys(spf3, "F")

        def open_remote(pane: int) -> None:
            type_keys(pane, "!")
            try:
                wait_for("the Sessions box", lambda: in_sessions_box(pane), timeout=8)
            except AssertionError:
                type_keys(pane, "!")
                wait_for("the Sessions box", lambda: in_sessions_box(pane))
            # Its computer has this one session: a narrow box cuts its title.
            line = wait_for("the remote row", lambda: row_of_host(screen(pane), host2, ""))
            number = re.search(r"│\S?\s*(\d+) ", line).group(1)
            for key in number + "g":
                type_keys(pane, key)
                time.sleep(0.1)
            wait_for("it picked", lambda: (row_of_host(screen(pane), host2, "") or "").startswith("│▌"))
            type_keys(pane, "\r")

        def beside(pane: int) -> list[int]:
            return [p["pane_id"] for p in panes() if p["tab_id"] == tab3 and p["pane_id"] != pane]

        open_remote(spf3)
        first_viewer = wait_for("the remote session beside Marmota", lambda: beside(spf3))
        type_keys(spf3, "\x1b")
        wait_for("the files", lambda: not in_sessions_box(spf3))
        type_keys(spf3, "q")
        wait_for("Marmota to quit", lambda: "marmota-quit" in screen(spf3))
        main_screen(spf3)
        type_keys(spf3, "F")
        time.sleep(3)  # its first refresh reaches the other computer
        open_remote(spf3)
        time.sleep(3)
        assert beside(spf3) == first_viewer, f"a second pane opened beside Marmota: {beside(spf3)} vs {first_viewer}"
        type_keys(spf3, "\x1b")
        subprocess.run(["tmux", "-L", remote_socket2, "kill-server"], capture_output=True)
        print("ok   restarted in its pane, Marmota took back the pane beside it that shows the other computer's "
              "session; enter didn't open a second one")

        # 10. a session someone started on the other computer, outside
        # Marmota: y session adopt moves it in, with the same id
        outside = socket + "-outside"
        subprocess.run(["env", *[f"{k}={v}" for k, v in env.items()], "tmux", "-L", outside, "new-session", "-d",
                        "-c", str(project), "/bin/sh", "-c", "claude -- 'started by hand there'; exec sh"],
                       check=True)
        on3 = f"env MARMOTA_TMUX_SOCKET={socket}-remote3 /bin/sh -c"
        listing = wait_for("it among those adopt can move", lambda: next(
            (line for line in y("adopt", "--on", on3, "--list").splitlines() if "started by hand" in line
             or "local" in line), None))
        outside_id = listing.split()[0]
        assert "moved" in y("adopt", "--on", on3, outside_id)
        wait_for("its terminal back at its shell", lambda: "claude" not in subprocess.run(
            ["tmux", "-L", outside, "list-panes", "-F", "#{pane_current_command}"],
            capture_output=True, text=True).stdout)
        wait_for("it in Marmota's tmux there", lambda: "cc-" + outside_id in subprocess.run(
            ["tmux", "-L", socket + "-remote3", "list-windows", "-a", "-F", "#{window_name}"],
            capture_output=True, text=True).stdout)
        assert any(s["id"] == outside_id for s in json.loads(y("list", "--json"))["sessions"])
        assert not any(l.startswith(outside_id) for l in y("adopt", "--on", on3, "--list").splitlines())
        for name in (outside, socket + "-remote3"):
            subprocess.run(["tmux", "-L", name, "kill-server"], capture_output=True)
        print("ok   y session adopt moved a session started by hand on the other computer into Marmota's tmux "
              "there, with the same id; its terminal went back to its shell")

        after = client_state()
        assert after == (before_workspace, before_focus), f"the test changed your window: {after}"
        print("ok   your active workspace and focused pane are unchanged")
        print("PASS")
        return 0
    except AssertionError as e:
        print(f"FAIL {e}")
        if spf and pane_by_id(spf):
            print("---- Marmota's screen ----")
            print(screen(spf))
        return 1
    finally:
        if not args.keep:
            for pane in panes():
                if is_test_pane(pane):
                    subprocess.run(["wezterm", "cli", "kill-pane", "--pane-id", str(pane["pane_id"])],
                                   capture_output=True)
            tmux("kill-server")
            subprocess.run(["tmux", "-L", socket + "-remote", "kill-server"], capture_output=True)
            # A killed server leaves its socket file behind.
            sockets = Path(os.environ.get("TMUX_TMPDIR", "/tmp")) / f"tmux-{os.getuid()}"
            subprocess.run(["tmux", "-L", socket + "-remote2", "kill-server"], capture_output=True)
            for name in ("-outside", "-remote3"):
                subprocess.run(["tmux", "-L", socket + name, "kill-server"], capture_output=True)
            for name in (socket, socket + "-remote", socket + "-remote2", socket + "-outside", socket + "-remote3"):
                (sockets / name).unlink(missing_ok=True)
            shutil.rmtree(work, ignore_errors=True)


if __name__ == "__main__":
    sys.exit(main())
