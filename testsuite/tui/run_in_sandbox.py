"""Run any command inside a fresh sandbox (temp HOME, XDG dirs, fakes on PATH).

Useful for tools that start spf themselves, such as the tmux based suite::

    cd testsuite
    .venv/bin/python -m tui.run_in_sandbox -- .venv/bin/python main.py --spf-path ../bin/y

The command inherits the sandbox environment (see tui.sandbox), so spf and
everything it launches read and write only the sandbox. Exit code is the
command's.
"""
from __future__ import annotations

import argparse
import subprocess
import sys
from pathlib import Path
from typing import List, Optional

from tui.sandbox import Sandbox


def main(argv: Optional[List[str]] = None) -> int:
    parser = argparse.ArgumentParser(prog="python -m tui.run_in_sandbox", description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--config-source", default="repo",
                        help="repo (default), binary, or a directory to copy config from")
    parser.add_argument("--binary", type=Path, default=None,
                        help="spf binary, needed for --config-source binary")
    parser.add_argument("--first-use", action="store_true",
                        help="let spf show its first-use intro (it swallows the first key)")
    parser.add_argument("--keep", action="store_true", help="keep the sandbox afterwards")
    parser.add_argument("--print-env", action="store_true", help="print the environment")
    parser.add_argument("command", nargs=argparse.REMAINDER, help="-- command [args...]")
    args = parser.parse_args(argv)
    command = args.command[1:] if args.command[:1] == ["--"] else args.command
    if not command:
        parser.error("no command given (use: -- command [args...])")

    sandbox = Sandbox(config_source=args.config_source, binary=args.binary,
                      first_use=args.first_use, keep=args.keep)
    with sandbox:
        env = sandbox.env()
        if args.print_env:
            for name in sorted(env):
                print(f"{name}={env[name]}")
        print(f"[run_in_sandbox] HOME={sandbox.home}", file=sys.stderr)
        code = subprocess.call(command, env=env)
        if args.keep:
            print(f"[run_in_sandbox] kept {sandbox.root}", file=sys.stderr)
    return code


if __name__ == "__main__":
    sys.exit(main())
