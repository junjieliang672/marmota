"""Tiny runner for pty-driven spf tests, with the same discovery rules as core/runner.py.

Any subclass of TuiTest whose name ends with ``Test`` in ``tui/tests/*_test.py``
is run. A test fails by raising (AssertionError, WaitTimeout, ...). On failure
the runner saves, for every session the test started, a PNG screenshot, the
screen text, the raw pty output, and spf's own log and fake-tool logs from the
sandbox into ``<artifacts>/<TestName>/``.
"""
from __future__ import annotations

import importlib
import logging
import shutil
import time
import traceback
from abc import ABC, abstractmethod
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, List, Mapping, Optional, Sequence, Tuple, Type

from tui.sandbox import Sandbox
from tui.session import PathLike, Session

logger = logging.getLogger("tui")

TUI_DIR: Path = Path(__file__).resolve().parent
DEFAULT_ARTIFACTS_DIR: Path = TUI_DIR / "artifacts"


@dataclass
class RunOptions:
    binary: Path
    artifacts: Path = DEFAULT_ARTIFACTS_DIR
    config_source: str = "repo"
    shots: bool = False
    keep_sandboxes: bool = False


class TuiContext:
    """Per-test factory for sandboxes and sessions, and the screenshot sink."""

    def __init__(self, options: RunOptions, test_name: str) -> None:
        self.options = options
        self.binary = options.binary
        self.artifacts = options.artifacts / test_name
        self._sandboxes: List[Sandbox] = []
        self._sessions: List[Session] = []
        shutil.rmtree(self.artifacts, ignore_errors=True)

    def sandbox(self, **options: Any) -> Sandbox:
        """A created Sandbox (see tui.sandbox.Sandbox), removed after the test."""
        options.setdefault("config_source", self.options.config_source)
        options.setdefault("binary", self.binary)
        options.setdefault("keep", self.options.keep_sandboxes)
        sandbox = Sandbox(**options).create()
        self._sandboxes.append(sandbox)
        return sandbox

    def session(self, cols: int = 120, rows: int = 40, cwd: Optional[PathLike] = None,
                env: Optional[Mapping[str, Optional[str]]] = None, *,
                sandbox: Optional[Sandbox] = None, **options: Any) -> Session:
        """A started Session; it gets a fresh sandbox unless one is passed."""
        session = Session(self.binary, cols, rows, cwd, env,
                          sandbox=sandbox or self.sandbox(), **options)
        self._sessions.append(session)
        return session.start()

    def snap(self, label: str, session: Optional[Session] = None) -> Path:
        """Save a screenshot now (default: the most recent session)."""
        target = session or self._last_session()
        path = target.screenshot(self.artifacts / f"{label}.png")
        logger.info("screenshot: %s", path)
        return path

    def _last_session(self) -> Session:
        if not self._sessions:
            raise RuntimeError("no session has been started in this test")
        return self._sessions[-1]

    def save_sessions(self, prefix: str, reason: str = "") -> List[Path]:
        """Screenshot, screen text and raw output of every session."""
        saved: List[Path] = []
        for index, session in enumerate(self._sessions):
            stem = self.artifacts / (f"{prefix}-{index}" if len(self._sessions) > 1 else prefix)
            try:
                saved.append(session.screenshot(stem.with_suffix(".png")))
            except Exception:  # pylint: disable=broad-except
                logger.exception("could not save screenshot for %r", session)
            text = stem.with_suffix(".txt")
            text.parent.mkdir(parents=True, exist_ok=True)
            details = (f"{session!r}\nreturncode={session.returncode} title={session.title!r} "
                       f"modes={sorted(session.modes)}\n"
                       f"queries answered={session.answered_queries}\n\n{reason}\n")
            text.write_text(details + session.dump() + "\n", "utf-8")
            saved.append(text)
            raw = stem.with_suffix(".raw")
            raw.write_bytes(session.raw_output())
            saved.append(raw)
        return saved

    def save_sandbox_logs(self) -> None:
        for index, sandbox in enumerate(self._sandboxes):
            target = self.artifacts / f"sandbox-{index}"
            try:
                if sandbox.spf_log_file.exists():
                    target.mkdir(parents=True, exist_ok=True)
                    shutil.copy2(sandbox.spf_log_file, target / "superfile.log")
                if any(sandbox.logs_dir.iterdir()):
                    shutil.copytree(sandbox.logs_dir, target / "fake-logs", dirs_exist_ok=True)
            except OSError:
                logger.exception("could not copy logs of %r", sandbox)

    def close(self) -> None:
        for session in self._sessions:
            session.close()
        for sandbox in self._sandboxes:
            if sandbox.keep:
                logger.info("kept sandbox %s", sandbox.root)
            sandbox.cleanup()


class TuiTest(ABC):
    """Base class for pty-driven tests. Implement run(); raise to fail."""

    def __init__(self, ctx: TuiContext) -> None:
        self.ctx = ctx
        self.logger = logging.getLogger("tui")

    def setup(self) -> None:
        """Optional preparation before run()."""

    @abstractmethod
    def run(self) -> None:
        """Drive spf and assert. Raise AssertionError (or anything) on failure."""

    def cleanup(self) -> None:
        """Optional cleanup; sessions and sandboxes are closed by the runner."""

    def __repr__(self) -> str:
        return self.__class__.__name__


@dataclass
class TuiResult:
    name: str
    passed: bool
    seconds: float
    error: str = ""
    artifacts: List[Path] = field(default_factory=list)


def get_testcases(only: Optional[Sequence[str]] = None) -> List[Type[TuiTest]]:
    found: List[Type[TuiTest]] = []
    for test_file in sorted((TUI_DIR / "tests").glob("*_test.py")):
        module = importlib.import_module(f"tui.tests.{test_file.stem}")
        for attr_name in dir(module):
            attr = getattr(module, attr_name)
            if (isinstance(attr, type) and issubclass(attr, TuiTest) and attr is not TuiTest
                    and attr_name.endswith("Test") and attr.__module__ == module.__name__):
                if only is None or attr_name in only:
                    found.append(attr)
    return found


def run_test(test_class: Type[TuiTest], options: RunOptions) -> TuiResult:
    name = test_class.__name__
    ctx = TuiContext(options, name)
    started = time.monotonic()
    error = ""
    details = ""
    test: Optional[TuiTest] = None
    try:
        test = test_class(ctx)
        test.setup()
        test.run()
    except Exception as exc:  # pylint: disable=broad-except
        error = f"{type(exc).__name__}: {exc}"
        details = traceback.format_exc()
        logger.debug("%s failed:\n%s", name, details)
    artifacts: List[Path] = []
    try:
        if error:
            artifacts = ctx.save_sessions("failure", details)
            ctx.save_sandbox_logs()
        elif options.shots:
            artifacts = ctx.save_sessions("final")
    finally:
        try:
            if test is not None:
                test.cleanup()
        finally:
            ctx.close()
    return TuiResult(name, not error, time.monotonic() - started, error, artifacts)


def run_tests(options: RunOptions, only: Optional[Sequence[str]] = None,
              stop_on_fail: bool = False) -> Tuple[int, int, List[TuiResult]]:
    """Run the discovered tests; return (passed, executed, results)."""
    results: List[TuiResult] = []
    testcases = get_testcases(only)
    logger.info("Testcases : %s", [t.__name__ for t in testcases])
    for test_class in testcases:
        logger.info("Running test %s", test_class.__name__)
        result = run_test(test_class, options)
        results.append(result)
        if result.passed:
            logger.info("Passed test %s (%.2fs)", result.name, result.seconds)
        else:
            logger.error("Failed test %s (%.2fs): %s", result.name, result.seconds,
                         result.error.splitlines()[0] if result.error else "")
            for path in result.artifacts:
                if path.suffix == ".png":
                    logger.error("  screenshot: %s", path)
            if stop_on_fail:
                break
    passed = sum(1 for r in results if r.passed)
    logger.info("Finished running %s test. %s passed", len(results), passed)
    return passed, len(results), results
