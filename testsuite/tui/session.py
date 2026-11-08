"""Drive a real spf binary in a pseudo-terminal, without tmux.

Example::

    with Session("bin/y", cols=150, rows=40, cwd=workdir) as spf:
        spf.wait_for("alpha.txt")
        spf.send("?")
        spf.wait_for("Open help menu")
        spf.screenshot("help.png")
        spf.send("esc", "q")
        assert spf.wait_exit() == 0

A background thread drains the pty continuously (spf blocks on a full pty
buffer otherwise), feeds the output to a pyte screen and answers terminal
queries (CSI 6n, OSC 10/11, DA1/DA2, ...) at the position they appear in the
stream, so cursor position reports are exact.
"""
from __future__ import annotations

import atexit
import codecs
import errno
import fcntl
import logging
import os
import select
import signal
import struct
import subprocess
import sys
import termios
import threading
import time
import weakref
from pathlib import Path
from typing import Callable, Dict, List, Mapping, Optional, Sequence, Set, Tuple, Union

import pyte

from tui import keys as keymod
from tui.sandbox import Sandbox
from tui.screen import (Cell, Needle, ScreenSnapshot, compile_needle, find_in_lines, row_cells,
                        row_text, search_line, snapshot_from_pyte)
from tui.terminal import Event, OutputFilter, Palette, TerminalState

PathLike = Union[str, "os.PathLike[str]"]
Key = Union[str, bytes]

logger = logging.getLogger(__name__)

# Pixel size of one cell, reported through TIOCSWINSZ and CSI 16t. It matches
# the default screenshot font, so image previews size themselves consistently.
DEFAULT_CELL_SIZE: Tuple[int, int] = (10, 20)
DEFAULT_KEY_DELAY: float = 0.05
# Bubble Tea reads "\x1b" followed quickly by "q" as alt+q, so keep a gap.
ESC_DELAY: float = 0.1
_RAW_OUTPUT_LIMIT: int = 16 * 1024 * 1024

# Runs in the child after setsid(): make the pty the controlling terminal
# (needed for /dev/tty, SIGWINCH and job control) and exec spf. This avoids
# Popen(preexec_fn=...), which is unsafe once reader threads are running.
_EXEC_WRAPPER: str = (
    "import fcntl, os, sys, termios\n"
    "fcntl.ioctl(0, termios.TIOCSCTTY, 0)\n"
    "os.execv(sys.argv[1], sys.argv[1:])\n"
)

_LIVE_SESSIONS: "weakref.WeakSet[Session]" = weakref.WeakSet()


class WaitTimeout(TimeoutError):
    """A wait_* call timed out. ``screen`` holds the screen text at that time."""

    def __init__(self, message: str, screen: str) -> None:
        super().__init__(f"{message}\n{screen}")
        self.screen = screen


class SessionExited(RuntimeError):
    """spf is no longer running, so input cannot be delivered."""


class _QuietScreen(pyte.Screen):
    """pyte screen that never answers queries itself; Session does that."""

    def write_process_input(self, data: str) -> None:
        return None


class Session:
    """spf running in a pty with an emulated screen. Use as a context manager.

    Args:
        binary: path to the spf binary.
        cols, rows: terminal size.
        cwd: working directory; spf opens it. Default: an empty dir in the sandbox.
        env: extra environment variables (None removes one), applied on top of
            the sandbox environment. HOME/XDG_* may only point inside the sandbox.
        args: extra command line arguments for spf.
        sandbox: a Sandbox to run in. When omitted the session creates its own
            and deletes it on close; a passed sandbox is left for the caller.
        palette: default colors of the fake terminal.
        cell_size: (width, height) in pixels reported to spf.
        key_delay: pause between keys in one send() call, so that every key
            arrives as its own read (Bubble Tea merges runes read together).
    """

    def __init__(self, binary: PathLike, cols: int = 120, rows: int = 40,
                 cwd: Optional[PathLike] = None,
                 env: Optional[Mapping[str, Optional[str]]] = None, *,
                 args: Sequence[str] = (), sandbox: Optional[Sandbox] = None,
                 palette: Optional[Palette] = None,
                 cell_size: Tuple[int, int] = DEFAULT_CELL_SIZE,
                 key_delay: float = DEFAULT_KEY_DELAY) -> None:
        self.binary: Path = Path(binary).expanduser()
        self.cols = cols
        self.rows = rows
        self.cwd: Optional[Path] = Path(cwd) if cwd is not None else None
        self.extra_env: Dict[str, Optional[str]] = dict(env or {})
        self.args: List[str] = list(args)
        self.palette: Palette = palette or Palette()
        self.key_delay = key_delay
        self._owns_sandbox = sandbox is None
        self._sandbox: Optional[Sandbox] = sandbox
        self._term = TerminalState(cols, rows, cell_size[0], cell_size[1], self.palette)
        self._screen = _QuietScreen(cols, rows)
        self._stream = pyte.Stream(self._screen)
        self._filter = OutputFilter()
        self._decoder = codecs.getincrementaldecoder("utf-8")(errors="replace")
        self._cond = threading.Condition(threading.RLock())
        self._write_lock = threading.Lock()
        self._stop = threading.Event()
        self._proc: Optional[subprocess.Popen] = None
        self._fd: Optional[int] = None
        self._reader: Optional[threading.Thread] = None
        self._eof = False
        self._closed = False
        self._generation = 0
        self._lines_cache: Optional[Tuple[int, List[Tuple[str, List[int]]]]] = None
        self._raw = bytearray()
        self._title = ""
        self._last_write = 0.0
        self._last_was_esc = False
        self.answered_queries: List[str] = []
        self.osc52_clipboard: List[str] = []
        self.graphics: List[Event] = []

    # ----- lifecycle -----------------------------------------------------------

    def __enter__(self) -> "Session":
        return self.start()

    def __exit__(self, *exc_info: object) -> None:
        self.close()

    def start(self) -> "Session":
        """Launch spf. Called by ``with``; returns self."""
        if self._proc is not None:
            return self
        binary = self.binary.resolve()
        if not binary.is_file() or not os.access(binary, os.X_OK):
            raise FileNotFoundError(f"spf binary not found or not executable: {binary}")
        sandbox = self._ensure_sandbox()
        cwd = self.cwd.resolve() if self.cwd is not None else sandbox.workdir()
        env = sandbox.env(self.extra_env)
        env["PWD"] = str(cwd)
        master, slave = os.openpty()
        try:
            self._set_winsize(slave)
            self._proc = subprocess.Popen(
                [sys.executable, "-I", "-c", _EXEC_WRAPPER, str(binary), *self.args],
                stdin=slave, stdout=slave, stderr=slave, cwd=str(cwd), env=env,
                start_new_session=True, close_fds=True)
        except BaseException:
            os.close(master)
            raise
        finally:
            os.close(slave)
        os.set_blocking(master, False)
        self._fd = master
        logger.debug("started %s (pid %s) in %s, sandbox %s", binary, self._proc.pid, cwd,
                     sandbox.root)
        self._reader = threading.Thread(target=self._read_loop, daemon=True,
                                        name=f"tui-reader-{self._proc.pid}")
        self._reader.start()
        _LIVE_SESSIONS.add(self)
        return self

    def close(self) -> None:
        """Stop spf (SIGTERM, then SIGKILL) and release the pty and sandbox."""
        if self._closed:
            return
        self._closed = True
        proc = self._proc
        if proc is not None and proc.poll() is None:
            _signal_group(proc, signal.SIGTERM)
            try:
                proc.wait(1.0)
            except subprocess.TimeoutExpired:
                _signal_group(proc, signal.SIGKILL)
                try:
                    proc.wait(2.0)
                except subprocess.TimeoutExpired:
                    logger.warning("pid %s did not die after SIGKILL", proc.pid)
        self._stop.set()
        if self._reader is not None:
            self._reader.join(2.0)
        if self._fd is not None:
            # Closing the master hangs up the pty, so leftover children get SIGHUP.
            os.close(self._fd)
            self._fd = None
        if self._owns_sandbox and self._sandbox is not None:
            self._sandbox.cleanup()
        _LIVE_SESSIONS.discard(self)

    def _ensure_sandbox(self) -> Sandbox:
        if self._sandbox is None:
            self._sandbox = Sandbox(binary=self.binary)
        return self._sandbox.create()

    @property
    def sandbox(self) -> Sandbox:
        return self._ensure_sandbox()

    @property
    def pid(self) -> Optional[int]:
        return self._proc.pid if self._proc is not None else None

    @property
    def alive(self) -> bool:
        return self._proc is not None and self._proc.poll() is None

    @property
    def returncode(self) -> Optional[int]:
        return self._proc.poll() if self._proc is not None else None

    def _set_winsize(self, fd: int) -> None:
        packed = struct.pack("HHHH", self.rows, self.cols, self.cols * self._term.cell_width,
                             self.rows * self._term.cell_height)
        fcntl.ioctl(fd, termios.TIOCSWINSZ, packed)

    def resize(self, cols: int, rows: int) -> None:
        """Change the terminal size; spf gets SIGWINCH and redraws."""
        with self._cond:
            self.cols, self.rows = cols, rows
            self._term.cols, self._term.rows = cols, rows
            self._screen.resize(rows, cols)
            self._touch()
        if self._fd is not None:
            self._set_winsize(self._fd)
        if self.alive and self._proc is not None:
            _signal_group(self._proc, signal.SIGWINCH)

    # ----- output side ---------------------------------------------------------

    def _read_loop(self) -> None:
        fd = self._fd
        assert fd is not None
        while not self._stop.is_set():
            try:
                ready, _, _ = select.select([fd], [], [], 0.05)
            except (OSError, ValueError):
                break
            if not ready:
                continue
            try:
                data = os.read(fd, 65536)
            except BlockingIOError:
                continue
            except OSError:
                break  # EIO: every slave fd is closed, spf has exited
            if not data:
                break
            self._handle_output(data)
        with self._cond:
            self._eof = True
            self._cond.notify_all()

    def _handle_output(self, data: bytes) -> None:
        replies: List[str] = []
        with self._cond:
            if len(self._raw) < _RAW_OUTPUT_LIMIT:
                self._raw += data
            for event in self._filter.feed(self._decoder.decode(data)):
                if event.kind == "text":
                    self._stream.feed(event.data)
                elif event.kind == "query":
                    cursor = self._screen.cursor
                    reply = self._term.answer(event, cursor.y, cursor.x)
                    self.answered_queries.append(event.data)
                    if reply:
                        replies.append(reply)
                elif event.kind == "mode":
                    self._term.apply_mode(event.data, event.arg)
                elif event.kind == "title":
                    self._title = event.data
                elif event.kind == "clipboard":
                    self.osc52_clipboard.append(event.data)
                elif event.kind == "graphics":
                    self.graphics.append(event)
            self._touch()
        for reply in replies:
            try:
                self._write(reply.encode("utf-8"), timeout=2.0)
            except (SessionExited, TimeoutError):
                logger.debug("could not deliver reply %r", reply)

    def _touch(self) -> None:
        self._generation += 1
        self._lines_cache = None
        self._cond.notify_all()

    # ----- input side ----------------------------------------------------------

    def _write(self, data: bytes, timeout: float = 5.0) -> None:
        with self._write_lock:
            if self._fd is None:
                raise SessionExited("session is closed")
            view = memoryview(data)
            deadline = time.monotonic() + timeout
            while view:
                try:
                    written = os.write(self._fd, view)
                    view = view[written:]
                except BlockingIOError:
                    remaining = deadline - time.monotonic()
                    if remaining <= 0:
                        raise TimeoutError("spf is not reading its input") from None
                    select.select([], [self._fd], [], min(remaining, 0.05))
                except OSError as exc:
                    if exc.errno in (errno.EIO, errno.EBADF):
                        raise SessionExited("spf has exited") from exc
                    raise

    def _pace(self, gap: float) -> None:
        wait = self._last_write + gap - time.monotonic()
        if wait > 0:
            time.sleep(wait)

    def _deliver(self, payload: bytes, delay: float) -> None:
        if not self.alive:
            raise SessionExited(f"spf has exited (code {self.returncode})")
        self._pace(max(delay, ESC_DELAY) if self._last_was_esc else delay)
        self._write(payload)
        self._last_write = time.monotonic()
        self._last_was_esc = payload == b"\x1b"

    @staticmethod
    def encode(key: Key) -> bytes:
        """Bytes for one key: a key spec (see tui.keys), literal text, or raw bytes."""
        if isinstance(key, bytes):
            return key
        encoded = keymod.encode_key(key)
        return encoded if encoded is not None else key.encode("utf-8")

    def send(self, *keys: Key, delay: Optional[float] = None) -> "Session":
        """Press keys one by one, e.g. ``send("J", "J", "ctrl+k", "enter")``.

        A str is a key spec ("enter", "esc", "tab", "shift+tab", "up",
        "pgdown", "space", "backspace", "ctrl+x", "alt+x", "alt+shift+right",
        a single character...) or, if it is not one, literal text. bytes are
        written verbatim. Use type_text() for text that could look like a key.
        """
        gap = self.key_delay if delay is None else delay
        payloads = [self.encode(key) for key in keys]
        for payload in payloads:
            logger.debug("send %r", payload)
            self._deliver(payload, gap)
        return self

    def type_text(self, text: str) -> "Session":
        """Write text in one go (Bubble Tea receives it as one runes message)."""
        self._deliver(text.encode("utf-8"), self.key_delay)
        return self

    def send_raw(self, data: bytes) -> "Session":
        self._deliver(data, 0.0)
        return self

    def paste(self, text: str) -> "Session":
        """Paste text, bracketed if spf enabled bracketed paste (mode 2004)."""
        if 2004 in self.modes:
            text = f"\x1b[200~{text}\x1b[201~"
        return self.send_raw(text.encode("utf-8"))

    def focus(self, focused: bool = True) -> "Session":
        """Report terminal focus in/out (spf enables focus reporting)."""
        return self.send_raw(b"\x1b[I" if focused else b"\x1b[O")

    def click(self, row: int, col: int, button: int = 0) -> "Session":
        """SGR mouse press+release at a 0-based cell (button 0=left, 2=right)."""
        self.send_raw(f"\x1b[<{button};{col + 1};{row + 1}M".encode())
        return self.send_raw(f"\x1b[<{button};{col + 1};{row + 1}m".encode())

    def scroll(self, row: int, col: int, up: bool = True) -> "Session":
        return self.send_raw(f"\x1b[<{64 if up else 65};{col + 1};{row + 1}M".encode())

    # ----- screen access -------------------------------------------------------

    def _lines_locked(self) -> List[Tuple[str, List[int]]]:
        cache = self._lines_cache
        if cache is not None and cache[0] == self._generation:
            return cache[1]
        lines = [row_text(self._screen, row) for row in range(self._screen.lines)]
        self._lines_cache = (self._generation, lines)
        return lines

    def screen_text(self) -> List[str]:
        """The screen as one string per row (wide-char stub cells skipped)."""
        with self._cond:
            return [text for text, _ in self._lines_locked()]

    def text(self) -> str:
        """The screen as a single string, trailing spaces stripped."""
        return "\n".join(line.rstrip() for line in self.screen_text())

    def dump(self) -> str:
        """Numbered screen text, for logs and error messages."""
        width = len(str(self.rows))
        lines = [f"{index:>{width}}|{line.rstrip()}"
                 for index, line in enumerate(self.screen_text())]
        border = "-" * min(self.cols + width + 1, 120)
        return "\n".join([border] + lines + [border])

    def find(self, needle: Needle) -> Optional[Tuple[int, int]]:
        """(row, col) of the first match of a str or compiled regex, or None."""
        hits = self.find_all(needle)
        return hits[0] if hits else None

    def find_all(self, needle: Needle) -> List[Tuple[int, int]]:
        compiled = compile_needle(needle)
        with self._cond:
            return find_in_lines(self._lines_locked(), compiled)

    def contains(self, needle: Needle) -> bool:
        return self.find(needle) is not None

    def cell(self, row: int, col: int) -> Cell:
        with self._cond:
            return row_cells(self._screen, row, self.palette)[col]

    def row(self, row: int) -> List[Cell]:
        with self._cond:
            return row_cells(self._screen, row, self.palette)

    def cells_of(self, needle: Needle) -> List[Cell]:
        """Cells (no stubs) of the first match; raises LookupError if absent."""
        compiled = compile_needle(needle)
        with self._cond:
            for row, (text, columns) in enumerate(self._lines_locked()):
                span = search_line(text, compiled)
                if span is None:
                    continue
                cells = row_cells(self._screen, row, self.palette)
                wanted = sorted(set(columns[span[0]:span[1]]))
                return [cells[col] for col in wanted]
        raise LookupError(f"{needle!r} is not on screen\n{self.dump()}")

    @property
    def cursor(self) -> Tuple[int, int]:
        with self._cond:
            return self._screen.cursor.y, min(self._screen.cursor.x, self.cols - 1)

    @property
    def cursor_visible(self) -> bool:
        with self._cond:
            return not self._screen.cursor.hidden

    @property
    def title(self) -> str:
        with self._cond:
            return self._title

    @property
    def modes(self) -> Set[int]:
        """DEC private modes spf has enabled (1049 alt screen, 2004 paste...)."""
        with self._cond:
            return set(self._term.private_modes)

    def snapshot(self) -> ScreenSnapshot:
        with self._cond:
            return snapshot_from_pyte(self._screen, self.palette, self._title)

    def raw_output(self) -> bytes:
        """Everything spf wrote (first 16 MiB), for debugging rendering issues."""
        with self._cond:
            return bytes(self._raw)

    def screenshot(self, path: PathLike, **options: object) -> Path:
        """Render the current screen to a PNG. See tui.screenshot.render_png."""
        from tui.screenshot import render_png  # pylint: disable=import-outside-toplevel
        return render_png(self.snapshot(), path, **options)  # type: ignore[arg-type]

    # ----- waiting -------------------------------------------------------------

    def wait_for(self, needle: Needle, timeout: float = 5.0) -> Tuple[int, int]:
        """Wait until needle (str or compiled regex) is on screen; return (row, col)."""
        compiled = compile_needle(needle)
        deadline = time.monotonic() + timeout
        with self._cond:
            while True:
                hits = find_in_lines(self._lines_locked(), compiled)
                if hits:
                    return hits[0]
                remaining = deadline - time.monotonic()
                if remaining <= 0 or (self._eof and not self.alive):
                    break
                self._cond.wait(min(remaining, 0.1))
        state = "spf exited" if not self.alive else f"timed out after {timeout:g}s"
        raise WaitTimeout(f"{state} waiting for {needle!r}", self.dump())

    def wait_gone(self, needle: Needle, timeout: float = 5.0) -> None:
        """Wait until needle is no longer on screen."""
        compiled = compile_needle(needle)
        deadline = time.monotonic() + timeout
        with self._cond:
            while find_in_lines(self._lines_locked(), compiled):
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    raise WaitTimeout(f"timed out after {timeout:g}s waiting for {needle!r} "
                                      "to disappear", self.dump())
                self._cond.wait(min(remaining, 0.1))

    def wait_stable(self, timeout: float = 5.0, quiet: float = 0.3) -> None:
        """Wait until the screen text and cursor stop changing for ``quiet`` s.

        Attributes are ignored, so a blinking text cursor does not count as a
        change, but a spinner does. Prefer wait_for when you know what to expect.
        """
        deadline = time.monotonic() + timeout
        last: Optional[Tuple[Tuple[str, ...], Tuple[int, int]]] = None
        changed = time.monotonic()
        with self._cond:
            while True:
                now = time.monotonic()
                current = (tuple(text for text, _ in self._lines_locked()),
                           (self._screen.cursor.y, self._screen.cursor.x))
                if current != last:
                    last, changed = current, now
                elif now - changed >= quiet:
                    return
                if now >= deadline:
                    raise WaitTimeout(f"screen still changing after {timeout:g}s", self.dump())
                self._cond.wait(min(quiet / 3, max(deadline - now, 0.01)))

    def wait_exit(self, timeout: float = 5.0) -> int:
        """Wait for spf to exit and return its exit code."""
        if self._proc is None:
            raise RuntimeError("session was never started")
        try:
            code = self._proc.wait(timeout)
        except subprocess.TimeoutExpired:
            raise WaitTimeout(f"spf still running after {timeout:g}s", self.dump()) from None
        if self._reader is not None:
            self._reader.join(1.0)  # pick up the final frames
        return code

    def __repr__(self) -> str:
        return (f"{self.__class__.__name__}(binary={self.binary}, size={self.cols}x{self.rows}, "
                f"pid={self.pid}, alive={self.alive})")


def wait_until(predicate: Callable[[], bool], timeout: float = 5.0,
               interval: float = 0.05) -> bool:
    """Poll predicate until it is true or timeout passes; return its last value.

    For side effects that are not on screen: fake tool logs, the sandbox
    clipboard, files spf writes.
    """
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if predicate():
            return True
        time.sleep(interval)
    return bool(predicate())


def _signal_group(proc: subprocess.Popen, sig: int) -> None:
    """Signal spf's process group; spf is its leader (start_new_session)."""
    try:
        os.killpg(proc.pid, sig)
    except ProcessLookupError:
        pass
    except PermissionError:
        # macOS refuses to signal a group that only holds zombies.
        try:
            proc.send_signal(sig)
        except ProcessLookupError:
            pass


def _close_all_sessions() -> None:
    for session in list(_LIVE_SESSIONS):
        try:
            session.close()
        except Exception:  # pylint: disable=broad-except
            logger.exception("failed to close %r at exit", session)


atexit.register(_close_all_sessions)
