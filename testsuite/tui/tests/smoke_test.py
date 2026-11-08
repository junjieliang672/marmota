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
    spf = test.ctx.session(cols=150, rows=40, cwd=work, sandbox=sandbox)
    spf.wait_for("alpha.txt", timeout=STARTUP_TIMEOUT)
    return spf, sandbox, work


def theme_color(sandbox: Sandbox, key: str) -> str:
    """Look up a color of the theme configured in the sandbox's config.toml."""
    config = (sandbox.spf_config_dir / "config.toml").read_text("utf-8")
    theme = re.search(r'^theme\s*=\s*"([^"]+)"', config, re.MULTILINE)
    assert theme is not None, "no theme in config.toml"
    text = (sandbox.spf_config_dir / "theme" / f"{theme.group(1)}.toml").read_text("utf-8")
    color = re.search(rf'^{re.escape(key)}\s*=\s*"(#[0-9A-Fa-f]{{6}})"', text, re.MULTILINE)
    assert color is not None, f"theme {theme.group(1)} has no {key}"
    return color.group(1).lower()


def has_cursor_marker(spf: Session, name: str, cursor_fg: str) -> bool:
    """True if the file panel cursor glyph is drawn just before entry ``name``."""
    pos = spf.find(name)
    assert pos is not None, f"{name} is not on screen\n{spf.dump()}"
    row, col = pos
    return any(cell.fg == cursor_fg and cell.char.strip()
               for cell in spf.row(row)[max(0, col - 8):col])


class FilePanelStartupTest(TuiTest):
    """spf starts in a directory with known files and lists them in the file panel."""

    def run(self) -> None:
        spf, sandbox, work = start_in_workdir(self)
        # The footer shows "<cursor>/<count>" once the directory is read.
        spf.wait_for("1/3")
        # The rows may still be drawing when the counter shows: wait for each.
        for name in ("beta.md", "gamma_dir"):
            spf.wait_for(name)
        # The panel header shows the (front-truncated) path of the directory.
        assert spf.contains(f"{work.parent.name}/{work.name}"), \
            f"panel header does not show {work}\n{spf.dump()}"
        dir_pos = spf.find("gamma_dir")
        file_pos = spf.find("alpha.txt")
        assert dir_pos is not None and file_pos is not None
        assert dir_pos[0] < file_pos[0], "directories should be listed before files"
        assert wait_until(lambda: spf.title == "Marmota", timeout=3), f"unexpected window title {spf.title!r}"
        self.ctx.snap("file-panel", spf)

        # The cursor glyph, drawn in the theme's cursor color, starts on the
        # first entry and follows 'j'.
        cursor_fg = theme_color(sandbox, "cursor")
        assert has_cursor_marker(spf, "gamma_dir", cursor_fg), \
            f"no {cursor_fg} cursor before gamma_dir\n{spf.dump()}"
        assert not has_cursor_marker(spf, "alpha.txt", cursor_fg)
        spf.send("j")
        spf.wait_for("2/3")
        assert wait_until(lambda: has_cursor_marker(spf, "alpha.txt", cursor_fg), timeout=3), \
            f"cursor did not move to alpha.txt\n{spf.dump()}"
        assert not has_cursor_marker(spf, "gamma_dir", cursor_fg)


class HelpMenuTest(TuiTest):
    """'?' opens the help menu, which lists the hotkey for itself."""

    def run(self) -> None:
        spf, _, _ = start_in_workdir(self)
        spf.send("?")
        row, _ = spf.wait_for("Open help menu (hotkeylist)")
        assert re.search(r"\?\s+Open help menu", spf.screen_text()[row]), \
            f"'?' is not shown as the help hotkey\n{spf.dump()}"
        assert spf.contains("Quit typing, modal or Marmota")
        self.ctx.snap("help-menu", spf)
        # The help search bar starts focused: the first esc blurs it, the
        # second one closes the menu.
        spf.send("esc", "esc")
        spf.wait_gone("Open help menu (hotkeylist)")
        assert spf.alive, "spf exited instead of closing the help menu"


class QuitTest(TuiTest):
    """'q' quits cleanly: exit code 0, alt screen left, cursor restored."""

    def run(self) -> None:
        spf, _, _ = start_in_workdir(self)
        assert 1049 in spf.modes, "spf did not switch to the alternate screen"
        spf.send("q")
        code = spf.wait_exit(timeout=5)
        assert code == 0, f"spf exited with {code}\n{spf.dump()}"
        assert 1049 not in spf.modes, "spf did not leave the alternate screen"
        assert spf.cursor_visible, "spf did not show the cursor again"
        assert b"Alas, there's been an error" not in spf.raw_output()


class SandboxIsolationTest(TuiTest):
    """spf keeps its state in the sandbox, and copying a path hits the fake pbcopy."""

    def run(self) -> None:
        spf, sandbox, work = start_in_workdir(self)
        spf.wait_for("1/3")
        spf.send("p")  # copy_path: the highlighted entry is gamma_dir
        expected = str(work / "gamma_dir")
        assert wait_until(lambda: sandbox.clipboard() == expected, timeout=3), \
            f"clipboard is {sandbox.clipboard()!r}, expected {expected!r}"
        assert sandbox.fake_calls("pbcopy") == [["pbcopy"]]
        spf.send("q")
        assert spf.wait_exit(timeout=5) == 0
        for path in (sandbox.spf_log_file, sandbox.spf_data_dir / "pinned.json",
                     sandbox.spf_config_dir / "config.toml"):
            assert path.is_file(), f"spf did not use the sandbox: {path} is missing"
        config = (sandbox.spf_config_dir / "config.toml").read_text("utf-8")
        assert "codex_command" in config, "the config.toml seeded from the repo"
