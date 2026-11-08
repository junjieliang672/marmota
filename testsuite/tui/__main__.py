"""``python -m tui``: run the pty-driven spf tests (run from testsuite/). See README.md."""
from __future__ import annotations

import argparse
import logging
import os
import sys
from pathlib import Path

from tui.runner import DEFAULT_ARTIFACTS_DIR, RunOptions, run_tests
from tui.sandbox import REPO_ROOT


def configure_logging(debug: bool = False) -> None:
    # Same format as testsuite/main.py
    handler = logging.StreamHandler(sys.stdout)
    handler.setFormatter(logging.Formatter('[%(asctime)s - %(levelname)7s] %(message)s',
                                           datefmt='%Y-%m-%d %H:%M:%S'))
    root = logging.getLogger()
    root.addHandler(handler)
    root.setLevel(logging.DEBUG if debug else logging.INFO)
    logging.getLogger("PIL").setLevel(logging.WARNING)


def default_binary() -> Path:
    return Path(os.environ.get("SPF_BINARY") or REPO_ROOT / "bin" / "y")


def main() -> int:
    parser = argparse.ArgumentParser(prog="python -m tui",
                                     description="superfile pty test suite (no tmux)")
    parser.add_argument("-d", "--debug", action="store_true", help="Enable debug logging")
    parser.add_argument("--spf-path", type=Path, default=None,
                        help="spf binary under test (default: $SPF_BINARY, else ../bin/y)")
    parser.add_argument("-t", "--tests", nargs="+",
                        help="Only run these testcases (class names)")
    parser.add_argument("--artifacts", type=Path, default=DEFAULT_ARTIFACTS_DIR,
                        help="Where screenshots and failure dumps go (default: tui/artifacts)")
    parser.add_argument("--shots", action="store_true",
                        help="Also save a final screenshot of every session for passing tests")
    parser.add_argument("--config-source", choices=("repo", "binary"), default="repo",
                        help="Seed config from src/superfile_config (repo) or from the "
                             "defaults embedded in the binary (binary)")
    parser.add_argument("-x", "--exitfirst", action="store_true",
                        help="Stop after the first failing test")
    parser.add_argument("--keep-sandboxes", action="store_true",
                        help="Do not delete the temp HOME of each session")
    args = parser.parse_args()
    configure_logging(args.debug)

    binary = (args.spf_path or default_binary()).expanduser().resolve()
    if not binary.is_file():
        logging.error("spf binary not found: %s (use --spf-path or $SPF_BINARY)", binary)
        return 2
    logging.info("spf under test: %s", binary)
    options = RunOptions(binary=binary, artifacts=args.artifacts.resolve(),
                         config_source=args.config_source, shots=args.shots,
                         keep_sandboxes=args.keep_sandboxes)
    passed, executed, _ = run_tests(options, only=args.tests, stop_on_fail=args.exitfirst)
    return 0 if executed and passed == executed else 1


if __name__ == "__main__":
    sys.exit(main())
