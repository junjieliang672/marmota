"""A throwaway HOME for one spf run, so tests never see the user's real state.

How spf resolves its directories (src/config/fixed_variable.go, adrg/xdg):

* config: ``$HOME/.config/marmota`` on macOS, ``$XDG_CONFIG_HOME/marmota``
  elsewhere.
* data/state/cache: ``$XDG_{DATA,STATE,CACHE}_HOME/marmota``, which on macOS
  default to ``~/Library/Application Support`` and ``~/Library/Caches``.
* trash on macOS: ``$HOME/.Trash`` (if that rename fails spf falls back to
  Finder through osascript, which is why ``.Trash`` is created and osascript
  is faked).

The sandbox sets HOME and all four XDG variables into a fresh temp dir, builds
the environment from scratch (WEZTERM_*, CLAUDE*, TERM_PROGRAM and friends from
the parent are not inherited), and puts ``tui/fakes`` first on PATH.
"""
from __future__ import annotations

import json
import os
import re
import shutil
import subprocess
import tempfile
from pathlib import Path
from typing import Any, Dict, List, Mapping, Optional, Sequence, Tuple, Union

REPO_ROOT: Path = Path(__file__).resolve().parents[2]
REPO_CONFIG_DIR: Path = REPO_ROOT / "src" / "superfile_config"
FAKES_DIR: Path = Path(__file__).resolve().parent / "fakes"

# Applied on top of the seeded config.toml for every run (none now: spf no
# longer goes online by itself).
DEFAULT_CONFIG_OVERRIDES: Dict[str, Any] = {}

# These must point inside the sandbox; Sandbox.env refuses anything else.
PROTECTED_ENV_VARS: Tuple[str, ...] = (
    "HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME",
    "_ZO_DATA_DIR", "CLAUDE_CONFIG_DIR", "TMPDIR",
)
PASSTHROUGH_ENV_VARS: Tuple[str, ...] = ("USER", "LOGNAME")

TomlValue = Union[bool, int, float, str, Sequence[str]]
ConfigSource = Union[str, Path]
FileContent = Union[str, bytes]
FixtureValue = Union[str, Sequence[Any], Mapping[str, Any]]

_BINARY_CONFIG_CACHE: Dict[Tuple[str, float], Dict[str, bytes]] = {}


def claude_project_slug(project_dir: Union[str, Path]) -> str:
    """Claude Code's directory name for a project under ~/.claude/projects."""
    return re.sub(r"[^A-Za-z0-9]", "-", str(project_dir))


def toml_value(value: TomlValue) -> str:
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, (int, float)):
        return repr(value)
    if isinstance(value, str):
        return json.dumps(value)
    return "[" + ", ".join(json.dumps(str(item)) for item in value) + "]"


def apply_toml_overrides(text: str, overrides: Mapping[str, TomlValue]) -> str:
    """Replace (or add) top-level ``key = value`` lines in a flat TOML file."""
    lines: List[str] = text.splitlines(keepends=True)
    for key, value in overrides.items():
        rendered = f"{key} = {toml_value(value)}\n"
        pattern = re.compile(rf"^\s*{re.escape(key)}\s*=")
        for index, line in enumerate(lines):
            if pattern.match(line):
                lines[index] = rendered
                break
        else:
            table = next((i for i, line in enumerate(lines) if line.lstrip().startswith("[")),
                         None)
            if table is None:
                if lines and not lines[-1].endswith("\n"):
                    lines[-1] += "\n"
                lines.append(rendered)
            else:
                lines.insert(table, rendered)
    return "".join(lines)


def _minimal_env(home: Path, path: str) -> Dict[str, str]:
    return {
        "PATH": path,
        "HOME": str(home),
        "XDG_CONFIG_HOME": str(home / ".config"),
        "XDG_DATA_HOME": str(home / ".local" / "share"),
        "XDG_STATE_HOME": str(home / ".local" / "state"),
        "XDG_CACHE_HOME": str(home / ".cache"),
        "TERM": "dumb",
        "LANG": "en_US.UTF-8",
    }


def read_binary_default_config(binary: Union[str, Path], scratch: Path) -> Dict[str, bytes]:
    """Return the config.toml/hotkeys.toml/theme files embedded in ``binary``.

    spf writes its embedded defaults into a missing config dir before it
    starts the TUI, then exits because stdin is not a terminal. Running it
    once that way with a scratch HOME yields exactly the defaults the binary
    was built with, which avoids drift when the working tree's
    src/superfile_config has moved on since the binary was built.
    """
    binary_path = Path(binary).resolve()
    key = (str(binary_path), binary_path.stat().st_mtime)
    cached = _BINARY_CONFIG_CACHE.get(key)
    if cached is not None:
        return cached
    home = scratch / "home"
    work = scratch / "work"
    work.mkdir(parents=True, exist_ok=True)
    env = _minimal_env(home, os.pathsep.join([str(FAKES_DIR), os.environ.get("PATH", "")]))
    subprocess.run([str(binary_path), str(work)], cwd=work, env=env, stdin=subprocess.DEVNULL,
                   stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=30, check=False)
    config_dir = home / ".config" / "marmota"
    files: Dict[str, bytes] = {}
    for path in sorted(config_dir.rglob("*")):
        if path.is_file():
            files[str(path.relative_to(config_dir))] = path.read_bytes()
    if "config.toml" not in files or "hotkeys.toml" not in files:
        raise RuntimeError(f"{binary_path} did not write its default config into {config_dir}")
    _BINARY_CONFIG_CACHE[key] = files
    return files


class Sandbox:
    """Temp HOME with a seeded Marmota config, fake tools and fixture files.

    Args:
        config_source: "repo" copies src/superfile_config (config.toml,
            hotkeys.toml, theme/), "binary" extracts the defaults embedded in
            ``binary``, or a directory path to copy from.
        binary: spf binary, required for config_source="binary".
        config_overrides: ``key = value`` lines to set in config.toml, on top
            of DEFAULT_CONFIG_OVERRIDES.
        hotkey_overrides: lines to set in hotkeys.toml, e.g.
            ``{"quit": ["q", ""]}``.
        first_use: leave out ``firstUseCheck`` so spf shows its intro modal
            (which swallows the first key press).
        claude_home: True creates an empty ``~/.claude/projects``; a path
            copies that tree into ``~/.claude``. False leaves no ``~/.claude``
            (a machine without Claude Code). CLAUDE_CONFIG_DIR always points
            at the sandbox's ``~/.claude``.
        parent_dir: where to create the temp dir (default: system temp).
        keep: do not delete the sandbox on cleanup (for debugging).
    """

    def __init__(self, *, config_source: ConfigSource = "repo",
                 binary: Optional[Union[str, Path]] = None,
                 config_overrides: Optional[Mapping[str, TomlValue]] = None,
                 hotkey_overrides: Optional[Mapping[str, TomlValue]] = None,
                 first_use: bool = False,
                 claude_home: Union[bool, str, Path] = False,
                 parent_dir: Optional[Union[str, Path]] = None,
                 keep: bool = False) -> None:
        self.config_source = config_source
        self.binary = Path(binary) if binary is not None else None
        self.config_overrides: Dict[str, TomlValue] = dict(DEFAULT_CONFIG_OVERRIDES)
        self.config_overrides.update(config_overrides or {})
        self.hotkey_overrides: Dict[str, TomlValue] = dict(hotkey_overrides or {})
        self.first_use = first_use
        self.claude_home = claude_home
        self.parent_dir = Path(parent_dir) if parent_dir is not None else None
        self.keep = keep
        self._root: Optional[Path] = None

    # ----- lifecycle -----------------------------------------------------------

    def create(self) -> "Sandbox":
        if self._root is not None:
            return self
        if self.parent_dir is not None:
            self.parent_dir.mkdir(parents=True, exist_ok=True)
        # resolve(): macOS temp dirs live behind the /var -> /private/var symlink,
        # and spf reports real paths.
        self._root = Path(tempfile.mkdtemp(prefix="spf-tui-", dir=self.parent_dir)).resolve()
        for directory in (self.home, self.config_home, self.data_home, self.state_home,
                          self.cache_home, self.tmp_dir, self.logs_dir, self.fixtures_dir,
                          self.bin_dir, self.home / ".Trash", self.spf_data_dir):
            directory.mkdir(parents=True, exist_ok=True)
        self._seed_config()
        if not self.first_use:
            (self.spf_data_dir / "firstUseCheck").write_bytes(b"")
        self._seed_claude_home()
        self.set_claude_agents([])
        self.set_wezterm_panes([])
        self.set_wezterm_text("")
        return self

    def cleanup(self) -> None:
        if self._root is not None and not self.keep:
            shutil.rmtree(self._root, ignore_errors=True)
        self._root = None

    def __enter__(self) -> "Sandbox":
        return self.create()

    def __exit__(self, *exc_info: object) -> None:
        self.cleanup()

    # ----- paths ---------------------------------------------------------------

    @property
    def root(self) -> Path:
        if self._root is None:
            raise RuntimeError("sandbox has not been created (use create() or 'with')")
        return self._root

    @property
    def home(self) -> Path:
        return self.root / "home"

    @property
    def config_home(self) -> Path:
        return self.home / ".config"

    @property
    def data_home(self) -> Path:
        return self.home / ".local" / "share"

    @property
    def state_home(self) -> Path:
        return self.home / ".local" / "state"

    @property
    def cache_home(self) -> Path:
        return self.home / ".cache"

    @property
    def tmp_dir(self) -> Path:
        return self.root / "tmp"

    @property
    def logs_dir(self) -> Path:
        return self.root / "logs"

    @property
    def fixtures_dir(self) -> Path:
        return self.root / "fixtures"

    @property
    def bin_dir(self) -> Path:
        """Per-sandbox executables; first on PATH, see add_fake()."""
        return self.root / "bin"

    @property
    def spf_config_dir(self) -> Path:
        return self.config_home / "marmota"

    @property
    def spf_data_dir(self) -> Path:
        return self.data_home / "marmota"

    @property
    def spf_state_dir(self) -> Path:
        return self.state_home / "marmota"

    @property
    def spf_log_file(self) -> Path:
        return self.spf_state_dir / "marmota.log"

    @property
    def claude_dir(self) -> Path:
        return self.home / ".claude"

    @property
    def clipboard_file(self) -> Path:
        return self.root / "clipboard.txt"

    def fake_log(self, name: str) -> Path:
        return self.logs_dir / f"{name}.jsonl"

    # ----- environment ---------------------------------------------------------

    def env(self, extra: Optional[Mapping[str, Optional[str]]] = None) -> Dict[str, str]:
        """Environment for a process inside the sandbox.

        ``extra`` is applied last; a None value removes a variable. Protected
        variables (HOME, XDG_*, ...) may only point inside the sandbox.
        """
        path = os.pathsep.join([str(self.bin_dir), str(FAKES_DIR),
                                os.environ.get("PATH", os.defpath)])
        env = _minimal_env(self.home, path)
        env.update({
            "TERM": "xterm-256color",
            "COLORTERM": "truecolor",
            "RUNEWIDTH_EASTASIAN": "0",
            "SHELL": "/bin/sh",
            "TMPDIR": str(self.tmp_dir),
            "_ZO_DATA_DIR": str(self.data_home / "zoxide"),
            "FAKE_LOG_DIR": str(self.logs_dir),
            "FAKE_CLAUDE_LOG": str(self.fake_log("claude")),
            "FAKE_CLAUDE_QUERY_LOG": str(self.fake_log("claude-queries")),
            "FAKE_CLAUDE_AGENTS_JSON": str(self.fixtures_dir / "claude-agents.json"),
            "FAKE_WEZTERM_LOG": str(self.fake_log("wezterm")),
            "FAKE_WEZTERM_LIST": str(self.fixtures_dir / "wezterm-list.json"),
            "FAKE_WEZTERM_TEXT": str(self.fixtures_dir / "wezterm-text.txt"),
            "FAKE_WEZTERM_STATE": str(self.root / "wezterm-next-pane-id"),
            "FAKE_CLIPBOARD_FILE": str(self.clipboard_file),
            # spf reads transcripts from $CLAUDE_CONFIG_DIR/projects, falling
            # back to $HOME/.claude/projects; both point into the sandbox.
            "CLAUDE_CONFIG_DIR": str(self.claude_dir),
        })
        for name in PASSTHROUGH_ENV_VARS:
            if name in os.environ:
                env[name] = os.environ[name]
        for name, value in (extra or {}).items():
            if value is None:
                env.pop(name, None)
            else:
                env[name] = value
        self._check_protected(env)
        return env

    def _check_protected(self, env: Mapping[str, str]) -> None:
        for name in PROTECTED_ENV_VARS:
            value = env.get(name)
            if value is None:
                continue
            resolved = Path(value).resolve()
            if resolved != self.root and self.root not in resolved.parents:
                raise ValueError(f"{name}={value} points outside the sandbox {self.root}; "
                                 "refusing to run spf against real user state")

    # ----- seeding -------------------------------------------------------------

    def _seed_config(self) -> None:
        files = self._config_files()
        for relative, content in files.items():
            target = self.spf_config_dir / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(content)
        config = self.spf_config_dir / "config.toml"
        config.write_text(apply_toml_overrides(config.read_text("utf-8"),
                                               self.config_overrides), "utf-8")
        if self.hotkey_overrides:
            hotkeys = self.spf_config_dir / "hotkeys.toml"
            hotkeys.write_text(apply_toml_overrides(hotkeys.read_text("utf-8"),
                                                    self.hotkey_overrides), "utf-8")

    def _config_files(self) -> Dict[str, bytes]:
        if self.config_source == "binary":
            if self.binary is None:
                raise ValueError("config_source='binary' needs binary=<path to spf>")
            scratch = self.root / "bootstrap"
            try:
                return read_binary_default_config(self.binary, scratch)
            finally:
                shutil.rmtree(scratch, ignore_errors=True)
        source = REPO_CONFIG_DIR if self.config_source == "repo" else Path(self.config_source)
        files: Dict[str, bytes] = {}
        for name in ("config.toml", "hotkeys.toml"):
            files[name] = (source / name).read_bytes()
        theme_dir = source / "theme"
        if theme_dir.is_dir():
            for theme in sorted(theme_dir.glob("*.toml")):
                files[f"theme/{theme.name}"] = theme.read_bytes()
        return files

    def _seed_claude_home(self) -> None:
        if not self.claude_home:
            return
        if isinstance(self.claude_home, (str, Path)):
            shutil.copytree(Path(self.claude_home), self.claude_dir, dirs_exist_ok=True)
        (self.claude_dir / "projects").mkdir(parents=True, exist_ok=True)

    # ----- helpers for tests ---------------------------------------------------

    def workdir(self, name: str = "work", files: Optional[Mapping[str, FileContent]] = None,
                dirs: Sequence[str] = ()) -> Path:
        """Create ``<root>/<name>`` with the given files and directories."""
        base = self.root / name
        base.mkdir(parents=True, exist_ok=True)
        for directory in dirs:
            (base / directory).mkdir(parents=True, exist_ok=True)
        for relative, content in (files or {}).items():
            target = base / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            if isinstance(content, bytes):
                target.write_bytes(content)
            else:
                target.write_text(content, "utf-8")
        return base

    def add_fake(self, name: str, script: str) -> Path:
        """Add an executable to the sandbox's own bin dir (first on PATH)."""
        target = self.bin_dir / name
        target.write_text(script, "utf-8")
        target.chmod(0o755)
        return target

    def fake_calls(self, name: str) -> List[List[str]]:
        """argv lists (argv[0] is the tool name) logged by a fake tool."""
        log = self.fake_log(name)
        if not log.exists():
            return []
        calls: List[List[str]] = []
        for line in log.read_text("utf-8").splitlines():
            if line.strip():
                calls.append(json.loads(line))
        return calls

    def clipboard(self) -> str:
        """What spf copied through the fake pbcopy."""
        if not self.clipboard_file.exists():
            return ""
        return self.clipboard_file.read_text("utf-8")

    def set_claude_agents(self, agents: FixtureValue) -> Path:
        """Output of the fake ``claude agents --json``; can change mid-session."""
        return self._write_fixture("claude-agents.json", agents)

    def set_wezterm_panes(self, panes: FixtureValue) -> Path:
        """Output of the fake ``wezterm cli list --format json``."""
        return self._write_fixture("wezterm-list.json", panes)

    def set_wezterm_text(self, text: str) -> Path:
        """Output of the fake ``wezterm cli get-text``."""
        target = self.fixtures_dir / "wezterm-text.txt"
        target.write_text(text, "utf-8")
        return target

    def _write_fixture(self, name: str, value: FixtureValue) -> Path:
        target = self.fixtures_dir / name
        target.write_text(value if isinstance(value, str) else json.dumps(value, indent=2), "utf-8")
        return target

    def add_claude_transcript(self, project_dir: Union[str, Path], session_id: str,
                              records: Sequence[Mapping[str, Any]]) -> Path:
        """Write ``~/.claude/projects/<slug>/<session_id>.jsonl`` in the sandbox."""
        target = self.claude_dir / "projects" / claude_project_slug(project_dir)
        target.mkdir(parents=True, exist_ok=True)
        path = target / f"{session_id}.jsonl"
        with path.open("w", encoding="utf-8") as handle:
            for record in records:
                handle.write(json.dumps(record) + "\n")
        return path

    def __repr__(self) -> str:
        return f"{self.__class__.__name__}(root={self._root})"
