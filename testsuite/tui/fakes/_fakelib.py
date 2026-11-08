"""Shared helpers for the fake tools in this directory (stdlib only, py3.6+).

Every fake appends its argv as one JSON array per line to a log file, so a
test can assert exactly what spf ran. Fixture variables (FAKE_*_JSON etc.)
hold either an absolute path to a file, whose current contents are used (so a
test can change them while spf runs), or the literal content itself.
"""
import fcntl
import json
import os
import sys


def log_call(env_var, argv0, default_name):
    """Append [argv0, *args] to $env_var, or to $FAKE_LOG_DIR/<default_name>.jsonl."""
    path = os.environ.get(env_var)
    if not path and os.environ.get("FAKE_LOG_DIR"):
        path = os.path.join(os.environ["FAKE_LOG_DIR"], default_name + ".jsonl")
    if not path:
        return
    line = json.dumps([argv0] + sys.argv[1:]) + "\n"
    with open(path, "a", encoding="utf-8") as handle:
        fcntl.flock(handle, fcntl.LOCK_EX)
        handle.write(line)


def read_source(env_var, default):
    """File contents if $env_var is an absolute path, else its literal value."""
    value = os.environ.get(env_var)
    if not value:
        return default
    if os.path.isabs(value):
        if not os.path.isfile(value):
            return default
        with open(value, encoding="utf-8") as handle:
            return handle.read()
    return value


def write_out(text):
    sys.stdout.write(text)
    if text and not text.endswith("\n"):
        sys.stdout.write("\n")
    sys.stdout.flush()


def next_counter(path, start):
    """Return the next value of a counter persisted in path (process safe)."""
    fd = os.open(path, os.O_RDWR | os.O_CREAT, 0o644)
    with os.fdopen(fd, "r+", encoding="utf-8") as handle:
        fcntl.flock(handle, fcntl.LOCK_EX)
        raw = handle.read().strip()
        value = int(raw) if raw.isdigit() else start
        handle.seek(0)
        handle.truncate()
        handle.write(str(value + 1))
    return value


def env_int(name, default):
    raw = os.environ.get(name, "")
    try:
        return int(raw)
    except ValueError:
        return default
