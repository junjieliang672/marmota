"""Take a screenshot of spf after a few key presses, inside a throwaway sandbox.

Run from testsuite/::

    python -m tui.shot --binary ../bin/y --size 150x40 --cwd DIR \\
        --keys "J J ctrl+k" --out shot.png

Key tokens are split like a shell command line. Besides key specs (see
tui/keys.py) and literal text, these tokens are understood:

    sleep:SECONDS   pause
    wait:TEXT       wait until TEXT is on screen
    stable          wait until the screen stops changing
    text:LITERAL    type LITERAL verbatim (even if it looks like a key name)
"""
from __future__ import annotations

import argparse
import sys
import time
from pathlib import Path
from typing import Dict, List, Optional, Tuple

from tui.keys import parse_sequence
from tui.sandbox import REPO_ROOT, Sandbox
from tui.session import Session, WaitTimeout


def parse_size(value: str) -> Tuple[int, int]:
    try:
        cols, rows = value.lower().split("x")
        return int(cols), int(rows)
    except ValueError:
        raise argparse.ArgumentTypeError(f"size must look like 150x40, got {value!r}") from None


def parse_env(pairs: List[str]) -> Dict[str, Optional[str]]:
    env: Dict[str, Optional[str]] = {}
    for pair in pairs:
        name, sep, value = pair.partition("=")
        env[name] = value if sep else None
    return env


def play(session: Session, tokens: List[str], settle: float, timeout: float) -> None:
    for token in tokens:
        if token.startswith("sleep:"):
            time.sleep(float(token[len("sleep:"):]))
        elif token.startswith("wait:"):
            session.wait_for(token[len("wait:"):], timeout=timeout)
        elif token == "stable":
            session.wait_stable(timeout=timeout, quiet=settle)
        elif token.startswith("text:"):
            session.type_text(token[len("text:"):])
        else:
            session.send(token)


def main(argv: Optional[List[str]] = None) -> int:
    parser = argparse.ArgumentParser(prog="python -m tui.shot", description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--binary", type=Path, default=REPO_ROOT / "bin" / "y",
                        help="spf binary (default: ../bin/y)")
    parser.add_argument("--size", type=parse_size, default=(150, 40), help="COLSxROWS")
    parser.add_argument("--cwd", type=Path, default=Path.cwd(),
                        help="directory spf opens (default: current dir)")
    parser.add_argument("--keys", default="", help='e.g. "J J ctrl+k" or "? wait:Help"')
    parser.add_argument("--out", type=Path, default=Path("shot.png"), help="PNG to write")
    parser.add_argument("--wait", default=None,
                        help="text to wait for before pressing keys (default: stable screen)")
    parser.add_argument("--settle", type=float, default=0.3,
                        help="quiet seconds before the shot (default 0.3)")
    parser.add_argument("--timeout", type=float, default=10.0, help="timeout for each wait")
    parser.add_argument("--delay", type=float, default=None, help="seconds between keys")
    parser.add_argument("--env", action="append", default=[], metavar="NAME=VALUE",
                        help="extra environment variable (NAME alone unsets it)")
    parser.add_argument("--config-source", choices=("repo", "binary"), default="repo")
    parser.add_argument("--font-size", type=int, default=16)
    parser.add_argument("--text", action="store_true", help="also print the screen text")
    args = parser.parse_args(argv)

    cols, rows = args.size
    tokens = parse_sequence(args.keys)
    with Sandbox(config_source=args.config_source, binary=args.binary) as sandbox:
        session = Session(args.binary, cols, rows, args.cwd, parse_env(args.env),
                          sandbox=sandbox)
        if args.delay is not None:
            session.key_delay = args.delay
        with session:
            try:
                if args.wait:
                    session.wait_for(args.wait, timeout=args.timeout)
                else:
                    session.wait_stable(timeout=args.timeout, quiet=0.5)
                play(session, tokens, args.settle, args.timeout)
                if session.alive:
                    session.wait_stable(timeout=args.timeout, quiet=args.settle)
            except WaitTimeout as exc:
                print(f"warning: {str(exc).splitlines()[0]}", file=sys.stderr)
            path = session.screenshot(args.out, font_size=args.font_size)
            if args.text:
                print(session.dump())
    print(path)
    return 0


if __name__ == "__main__":
    sys.exit(main())
