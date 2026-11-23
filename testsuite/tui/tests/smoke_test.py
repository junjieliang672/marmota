"""Smoke tests: start, help menu, quit, and sandbox isolation."""
from __future__ import annotations

import re
from pathlib import Path
from typing import Dict, Tuple

from tui.runner import TuiTest
from tui.sandbox import Sandbox
from tui.session import Session, wait_until

FILES: Dict[str, str] = {
    "alpha.txt": "hello\nworld\n",
    "beta.md": "# beta\n\nsome *markdown*\n",
}
DIRS: Tuple[str, ...] = ("gamma_dir",)
STARTUP_TIMEOUT: float = 10.0


def start_in_workdir(test: TuiTest) -> Tuple[Session, Sandbox, Path]:
    sandbox = test.ctx.sandbox()
    work = sandbox.workdir("work", files=FILES, dirs=DIRS)
    marmota = test.ctx.session(cols=150, rows=40, cwd=work, sandbox=sandbox)
    marmota.wait_for("alpha.txt", timeout=STARTUP_TIMEOUT)
    return marmota, sandbox, work


def theme_color(sandbox: Sandbox, key: str) -> str:
    """Look up a color of the theme configured in the sandbox's config.toml."""
    config = (sandbox.marmota_config_dir / "config.toml").read_text("utf-8")
    theme = re.search(r'^theme\s*=\s*"([^"]+)"', config, re.MULTILINE)
    assert theme is not None, "no theme in config.toml"
    text = (sandbox.marmota_config_dir / "theme" / f"{theme.group(1)}.toml").read_text("utf-8")
    color = re.search(rf'^{re.escape(key)}\s*=\s*"(#[0-9A-Fa-f]{{6}})"', text, re.MULTILINE)
    assert color is not None, f"theme {theme.group(1)} has no {key}"
    return color.group(1).lower()


def has_cursor_marker(marmota: Session, name: str, cursor_fg: str) -> bool:
    """True if the file panel cursor glyph is drawn just before entry ``name``."""
    pos = marmota.find(name)
    assert pos is not None, f"{name} is not on screen\n{marmota.dump()}"
    row, col = pos
    return any(cell.fg == cursor_fg and cell.char.strip()
               for cell in marmota.row(row)[max(0, col - 8):col])


class FilePanelStartupTest(TuiTest):
    """marmota starts in a directory with known files and lists them in the file panel."""

    def run(self) -> None:
        marmota, sandbox, work = start_in_workdir(self)
        # The footer shows "<cursor>/<count>" once the directory is read.
        marmota.wait_for("1/3")
        # The rows may still be drawing when the counter shows: wait for each.
        for name in ("beta.md", "gamma_dir"):
            marmota.wait_for(name)
        # The panel header shows the (front-truncated) path of the directory.
        assert marmota.contains(f"{work.parent.name}/{work.name}"), \
            f"panel header does not show {work}\n{marmota.dump()}"
        dir_pos = marmota.find("gamma_dir")
        file_pos = marmota.find("alpha.txt")
        assert dir_pos is not None and file_pos is not None
        assert dir_pos[0] < file_pos[0], "directories should be listed before files"
        assert wait_until(lambda: marmota.title == "Marmota", timeout=3), f"unexpected window title {marmota.title!r}"
        self.ctx.snap("file-panel", marmota)

        # The cursor glyph, drawn in the theme's cursor color, starts on the
        # first entry and follows 'j'.
        cursor_fg = theme_color(sandbox, "cursor")
        assert has_cursor_marker(marmota, "gamma_dir", cursor_fg), \
            f"no {cursor_fg} cursor before gamma_dir\n{marmota.dump()}"
        assert not has_cursor_marker(marmota, "alpha.txt", cursor_fg)
        marmota.send("j")
        marmota.wait_for("2/3")
        assert wait_until(lambda: has_cursor_marker(marmota, "alpha.txt", cursor_fg), timeout=3), \
            f"cursor did not move to alpha.txt\n{marmota.dump()}"
        assert not has_cursor_marker(marmota, "gamma_dir", cursor_fg)


class HelpMenuTest(TuiTest):
    """'?' opens the help menu, which lists the hotkey for itself."""

    def run(self) -> None:
        marmota, _, _ = start_in_workdir(self)
        marmota.send("?")
        row, _ = marmota.wait_for("Open help menu (hotkeylist)")
        assert re.search(r"\?\s+Open help menu", marmota.screen_text()[row]), \
            f"'?' is not shown as the help hotkey\n{marmota.dump()}"
        assert marmota.contains("Quit typing, modal or Marmota")
        self.ctx.snap("help-menu", marmota)
        # The help search bar starts focused: the first esc blurs it, the
        # second one closes the menu.
        marmota.send("esc", "esc")
        marmota.wait_gone("Open help menu (hotkeylist)")
        assert marmota.alive, "marmota exited instead of closing the help menu"


class QuitTest(TuiTest):
    """'q' quits cleanly: exit code 0, alt screen left, cursor restored."""

    def run(self) -> None:
        marmota, _, _ = start_in_workdir(self)
        assert 1049 in marmota.modes, "marmota did not switch to the alternate screen"
        marmota.send("q")
        code = marmota.wait_exit(timeout=5)
        assert code == 0, f"marmota exited with {code}\n{marmota.dump()}"
        assert 1049 not in marmota.modes, "marmota did not leave the alternate screen"
        assert marmota.cursor_visible, "marmota did not show the cursor again"
        assert b"Alas, there's been an error" not in marmota.raw_output()


class SandboxIsolationTest(TuiTest):
    """marmota keeps its state in the sandbox, and copying a path hits the fake pbcopy."""

    def run(self) -> None:
        marmota, sandbox, work = start_in_workdir(self)
        marmota.wait_for("1/3")
        marmota.send("p")  # copy_path: the highlighted entry is gamma_dir
        expected = str(work / "gamma_dir")
        assert wait_until(lambda: sandbox.clipboard() == expected, timeout=3), \
            f"clipboard is {sandbox.clipboard()!r}, expected {expected!r}"
        assert sandbox.fake_calls("pbcopy") == [["pbcopy"]]
        marmota.send("q")
        assert marmota.wait_exit(timeout=5) == 0
        for path in (sandbox.marmota_log_file, sandbox.marmota_data_dir / "pinned.json",
                     sandbox.marmota_config_dir / "config.toml"):
            assert path.is_file(), f"marmota did not use the sandbox: {path} is missing"
        config = (sandbox.marmota_config_dir / "config.toml").read_text("utf-8")
        assert "codex_command" in config, "the config.toml seeded from the repo"
