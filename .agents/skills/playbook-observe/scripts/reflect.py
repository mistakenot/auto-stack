#!/usr/bin/env python3
"""Shared helper for the reflection playbook (observe / refine / search).

Two responsibilities, both of which MUST be centralized so every skill behaves
identically:

  1. gen-id  — mint immutable, lexically time-ordered IDs. A microsecond UTC
               timestamp alone can collide, so each ID also carries a monotonic
               per-process counter. IDs minted in one process are strictly
               increasing; IDs minted across processes order by wall clock and
               only tie-break wrongly if two processes mint inside the same
               microsecond (acceptable for this telemetry).

  2. append  — append events to an NDJSON log (retrievals.ndjson) as the single
               serialized writer. Sharded matchers never write; the Search
               coordinator pipes complete events here. We take an advisory
               flock on `<file>.lock`, write each event as one complete line,
               fsync, and release. Concurrent parent sessions block on the same
               lock, so lines are never torn or interleaved.

Stdlib only — this ships inside the compiled skill and runs wherever the skill
is installed, with no project dependencies.

Usage:
    python3 reflect.py gen-id [--count N]
    cat events.ndjson | python3 reflect.py append --file docs/reflection/retrievals.ndjson
"""

from __future__ import annotations

import argparse
import datetime
import fcntl
import json
import os
import sys

ID_TIME_FMT = "%Y%m%dT%H%M%S"


def _utc_now() -> datetime.datetime:
    return datetime.datetime.now(datetime.timezone.utc)


class _IdMinter:
    """Mints strictly-increasing, lexically time-ordered IDs in one process."""

    def __init__(self) -> None:
        self._counter = 0

    def mint(self) -> str:
        self._counter += 1
        now = _utc_now()
        # YYYYMMDDThhmmss.ffffffZ-NNNN
        stamp = now.strftime(ID_TIME_FMT) + f".{now.microsecond:06d}Z"
        return f"{stamp}-{self._counter:04d}"


def cmd_gen_id(args: argparse.Namespace) -> int:
    minter = _IdMinter()
    out = "\n".join(minter.mint() for _ in range(max(1, args.count)))
    print(out)
    return 0


def _iter_events(raw: str):
    """Yield event dicts from stdin that is either NDJSON or a single JSON array."""
    stripped = raw.strip()
    if not stripped:
        return
    if stripped[0] == "[":
        for obj in json.loads(stripped):
            yield obj
        return
    for line in stripped.splitlines():
        line = line.strip()
        if line:
            yield json.loads(line)


def cmd_append(args: argparse.Namespace) -> int:
    raw = sys.stdin.read()
    try:
        events = list(_iter_events(raw))
    except json.JSONDecodeError as exc:
        print(f"reflect append: invalid JSON on stdin: {exc}", file=sys.stderr)
        return 2
    if not events:
        return 0

    minter = _IdMinter()
    lines: list[str] = []
    for evt in events:
        if not isinstance(evt, dict):
            print("reflect append: each event must be a JSON object", file=sys.stderr)
            return 2
        # Centralize correctness: stamp a real-clock time-ordered id / timestamp
        # when the caller omitted them, so every event in the log is comparable.
        evt.setdefault("event_id", minter.mint())
        evt.setdefault("occurred_at", _utc_now().strftime("%Y%m%dT%H%M%S")
                       + f".{_utc_now().microsecond:06d}Z")
        # One complete line, no embedded newlines.
        lines.append(json.dumps(evt, separators=(",", ":"), ensure_ascii=False))

    path = os.path.abspath(args.file)
    os.makedirs(os.path.dirname(path), exist_ok=True)
    lock_path = path + ".lock"

    # Advisory exclusive lock held for the whole append so concurrent writers
    # (other parent sessions) serialize. The data file is opened separately in
    # append mode so the OS positions every write at EOF.
    with open(lock_path, "w") as lock_f:
        fcntl.flock(lock_f, fcntl.LOCK_EX)
        try:
            with open(path, "a", encoding="utf-8") as data_f:
                data_f.write("".join(line + "\n" for line in lines))
                data_f.flush()
                os.fsync(data_f.fileno())
        finally:
            fcntl.flock(lock_f, fcntl.LOCK_UN)

    print(f"appended {len(lines)} event(s) to {args.file}")
    return 0


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="reflect", description=__doc__)
    sub = parser.add_subparsers(dest="cmd", required=True)

    p_id = sub.add_parser("gen-id", help="mint time-ordered IDs (one per line)")
    p_id.add_argument("--count", type=int, default=1, help="how many IDs to mint")
    p_id.set_defaults(func=cmd_gen_id)

    p_ap = sub.add_parser("append", help="atomically append NDJSON events under a lock")
    p_ap.add_argument("--file", required=True, help="target .ndjson log path")
    p_ap.set_defaults(func=cmd_append)

    args = parser.parse_args(argv)
    return args.func(args)


if __name__ == "__main__":
    raise SystemExit(main())
