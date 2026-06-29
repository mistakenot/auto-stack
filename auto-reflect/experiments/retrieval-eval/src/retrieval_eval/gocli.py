"""Thin wrapper over the real `auto reflect` Go CLI.

Used by the conformance harness to A/B the shipped matcher against the Python
baseline. `retrieve` output replaces the rule id with a freshly-minted
retrieval_id and exposes no score, so we map rows back to rule ids by use_when
(unique per rule) and compare *ordering*.
"""
from __future__ import annotations

import json
import subprocess
from typing import Sequence


class GoCLIError(RuntimeError):
    pass


def _run(args: list[str], cwd: str) -> str:
    proc = subprocess.run(
        ["auto", "reflect", *args],
        cwd=cwd,
        capture_output=True,
        text=True,
    )
    if proc.returncode != 0:
        raise GoCLIError(f"`auto reflect {' '.join(args)}` failed: {proc.stderr.strip()}")
    return proc.stdout


def retrieve_use_when(
    intent: str,
    cwd: str,
    domain: Sequence[str] | None = None,
    no_drafts: bool = False,
) -> list[str]:
    """Run retrieve; return the surfaced rules' use_when strings, in order."""
    args = ["retrieve", intent, "--limit", "0"]
    if domain:
        args += ["--domain", ",".join(domain)]
    if no_drafts:
        args.append("--no-drafts")
    out = _run(args, cwd)
    rows = json.loads(out)
    return [r["use_when"] for r in rows]


def retrieve_ids(
    intent: str,
    cwd: str,
    use_when_to_id: dict[str, str],
    domain: Sequence[str] | None = None,
    no_drafts: bool = False,
) -> list[str]:
    """Run retrieve and map surfaced rows back to rule ids via use_when."""
    ids: list[str] = []
    for uw in retrieve_use_when(intent, cwd, domain=domain, no_drafts=no_drafts):
        if uw not in use_when_to_id:
            raise GoCLIError(f"retrieve returned an unknown use_when: {uw!r}")
        ids.append(use_when_to_id[uw])
    return ids


def rule_create(
    cwd: str,
    use_when: str,
    content: str,
    causal_note: str,
    domain: Sequence[str],
    rule_type: str,
    lifecycle: str | None = None,
) -> str:
    """Create a rule in the store at cwd; return its assigned id."""
    args = [
        "rule", "create",
        "--use-when", use_when,
        "--content", content,
        "--causal-note", causal_note,
        "--domain", ",".join(domain),
        "--type", rule_type,
    ]
    if lifecycle:
        args += ["--lifecycle", lifecycle]
    out = _run(args, cwd)
    obj = json.loads(out)
    rid = obj.get("id") or obj.get("rule", {}).get("id")
    if not rid:
        raise GoCLIError(f"rule create returned no id: {out[:200]}")
    return rid


def rule_retire(cwd: str, rule_id: str) -> None:
    _run(["rule", "retire", rule_id], cwd)


def rule_promote(cwd: str, rule_id: str, force: bool = True) -> None:
    args = ["rule", "promote", rule_id]
    if force:
        args.append("--force")
    _run(args, cwd)
