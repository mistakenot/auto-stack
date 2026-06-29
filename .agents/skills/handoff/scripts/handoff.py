#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.12"
# ///
"""Local JSONL-backed stack for passing short handoff notes between sessions."""

from __future__ import annotations

import argparse
import datetime as dt
import fcntl
import json
import os
from pathlib import Path
import sys
from typing import TextIO


DEFAULT_FILE = Path(os.environ.get("HANDOFF_FILE", "~/.handoff.jsonl")).expanduser()


def _utc_now() -> str:
    return dt.datetime.now(dt.timezone.utc).isoformat(timespec="microseconds").replace("+00:00", "Z")


def _resolve_path(raw: str | None) -> Path:
    return Path(raw).expanduser() if raw else DEFAULT_FILE


def _lock(file: TextIO) -> None:
    fcntl.flock(file.fileno(), fcntl.LOCK_EX)


def _unlock(file: TextIO) -> None:
    fcntl.flock(file.fileno(), fcntl.LOCK_UN)


def _read_push_text(parts: list[str]) -> str:
    if parts and parts[0] == "--":
        parts = parts[1:]
    if parts:
        return " ".join(parts)

    if sys.stdin.isatty():
        raise ValueError("push requires text arguments or stdin")

    raw = sys.stdin.read()
    if raw.endswith("\n"):
        raw = raw[:-1]
    if not raw:
        raise ValueError("push received empty stdin")
    return raw


def cmd_push(args: argparse.Namespace) -> int:
    try:
        text = _read_push_text(args.text)
    except ValueError as exc:
        print(f"handoff push: {exc}", file=sys.stderr)
        return 2

    path = _resolve_path(args.file)
    path.parent.mkdir(parents=True, exist_ok=True)
    record = {"created_at": _utc_now(), "text": text}
    line = json.dumps(record, ensure_ascii=False, separators=(",", ":"))

    with path.open("a+", encoding="utf-8") as file:
        _lock(file)
        try:
            file.write(line + "\n")
            file.flush()
            os.fsync(file.fileno())
        finally:
            _unlock(file)

    print(f"pushed 1 item to {path}")
    return 0


def _load_stack(file: TextIO) -> list[str]:
    file.seek(0)
    return [line.rstrip("\n") for line in file.readlines() if line.strip()]


def cmd_pull(args: argparse.Namespace) -> int:
    path = _resolve_path(args.file)
    path.parent.mkdir(parents=True, exist_ok=True)

    with path.open("a+", encoding="utf-8") as file:
        _lock(file)
        try:
            lines = _load_stack(file)
            if not lines:
                print("handoff pop: stack is empty", file=sys.stderr)
                return 1

            raw = lines.pop()
            try:
                record = json.loads(raw)
            except json.JSONDecodeError as exc:
                print(f"handoff pop: invalid JSONL record at top of stack: {exc}", file=sys.stderr)
                return 2

            if not isinstance(record, dict):
                print("handoff pop: top JSONL record is not an object", file=sys.stderr)
                return 2

            file.seek(0)
            file.truncate()
            if lines:
                file.write("\n".join(lines) + "\n")
            file.flush()
            os.fsync(file.fileno())
        finally:
            _unlock(file)

    if args.json:
        print(json.dumps(record, ensure_ascii=False, separators=(",", ":")))
    else:
        print(str(record.get("text", "")))
    return 0


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="handoff",
        description="Push and pull short handoff notes using a local JSONL LIFO stack.",
    )
    parser.add_argument(
        "--file",
        help="JSONL stack file (default: HANDOFF_FILE or ~/.handoff.jsonl)",
    )

    sub = parser.add_subparsers(dest="command", required=True)

    push = sub.add_parser("push", help="append a note to the handoff stack")
    push.add_argument("text", nargs=argparse.REMAINDER, help="note text; reads stdin when omitted")
    push.set_defaults(func=cmd_push)

    pop = sub.add_parser("pop", aliases=["pull"], help="remove and print the latest note")
    pop.add_argument("--json", action="store_true", help="print the complete JSON object")
    pop.set_defaults(func=cmd_pull)

    return parser


def main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    return args.func(args)


if __name__ == "__main__":
    raise SystemExit(main())
