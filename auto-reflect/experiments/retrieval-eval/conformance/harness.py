"""Conformance harness: prove the Python baseline ranks identically to the Go CLI.

Both layers build a HERMETIC, throwaway `auto reflect` store and A/B the shipped
matcher against the Python baseline. Nothing here touches the live project store
— important, because `auto reflect retrieve` is NOT read-only: it appends a
`retrieval` event to the log on every call. Running against the live store would
pollute it with experiment artifacts.

  Layer 1 — real-corpus parity: rebuild the pinned 120-rule snapshot into a temp
            store, then compare across realistic queries.
  Layer 2 — synthetic edge cases: a temp store of rules crafted to exercise ties,
            hard injection, domain filters, and lifecycle transitions.

A mismatch = the two produced different surfaced-rule orderings for a query.
"""
from __future__ import annotations

import subprocess
import sys
import tempfile
from dataclasses import dataclass
from pathlib import Path

_SRC = Path(__file__).resolve().parents[1] / "src"
if str(_SRC) not in sys.path:
    sys.path.insert(0, str(_SRC))

from retrieval_eval import gocli  # noqa: E402
from retrieval_eval.baseline import Rule, match_rules, ordered_ids  # noqa: E402
from retrieval_eval.corpus import load_rules, use_when_to_id  # noqa: E402

import fixtures  # noqa: E402  (same dir)


@dataclass
class QueryResult:
    intent: str
    domain: list[str] | None
    no_drafts: bool
    go_ids: list[str]
    py_ids: list[str]

    @property
    def ok(self) -> bool:
        return self.go_ids == self.py_ids


def _check(rules: list[Rule], queries, cwd: str) -> list[QueryResult]:
    uw2id = use_when_to_id(rules)
    results: list[QueryResult] = []
    for intent, domain, no_drafts in queries:
        go_ids = gocli.retrieve_ids(intent, cwd, uw2id, domain=domain, no_drafts=no_drafts)
        py_ids = ordered_ids(
            match_rules(rules, intent, domain_filter=domain, include_drafts=not no_drafts)
        )
        results.append(QueryResult(intent, domain, no_drafts, go_ids, py_ids))
    return results


def _git(cwd: str, *args: str) -> None:
    subprocess.run(["git", *args], cwd=cwd, check=True, capture_output=True, text=True)


def _init_store(workdir: str) -> None:
    _git(workdir, "init", "-q")
    _git(workdir, "config", "user.email", "conformance@test.local")
    _git(workdir, "config", "user.name", "conformance")
    (Path(workdir) / "README.md").write_text("conformance fixture store\n")
    _git(workdir, "add", "-A")
    _git(workdir, "commit", "-q", "-m", "init")
    subprocess.run(
        ["auto", "reflect", "init", "--project"],
        cwd=workdir, capture_output=True, text=True, check=True,
    )


def build_store(workdir: str, specs: list[dict]) -> tuple[str, list[Rule]]:
    """Build a hermetic store from rule specs; return (cwd, rules-with-store-ids).

    A spec is {use_when, content, causal_note, domain, rule_type, [post]} where
    `post` is an optional lifecycle transition: "retire" -> stale, "promote" ->
    confirmed.
    """
    _init_store(workdir)
    rules: list[Rule] = []
    for spec in specs:
        rid = gocli.rule_create(
            workdir,
            use_when=spec["use_when"], content=spec.get("content", "c"),
            causal_note=spec.get("causal_note", "n"), domain=spec["domain"],
            rule_type=spec.get("rule_type", "soft"),
        )
        lifecycle = "draft"
        post = spec.get("post")
        if post == "retire":
            gocli.rule_retire(workdir, rid)
            lifecycle = "stale"
        elif post == "promote":
            gocli.rule_promote(workdir, rid, force=True)
            lifecycle = "confirmed"
        rules.append(Rule(
            id=rid, use_when=spec["use_when"], domain=list(spec["domain"]),
            rule_type=spec.get("rule_type", "soft"), lifecycle=lifecycle,
            content=spec.get("content", "c"), causal_note=spec.get("causal_note", "n"),
        ))
    return workdir, rules


def _snapshot_specs() -> list[dict]:
    """Convert the pinned snapshot rules into create-specs (all were draft)."""
    specs: list[dict] = []
    for r in load_rules():
        specs.append({
            "use_when": r.use_when, "content": r.content or "c",
            "causal_note": r.causal_note or "n", "domain": r.domain,
            "rule_type": r.rule_type,
        })
    return specs


def run_real_corpus_parity(repo_root: str | None = None) -> list[QueryResult]:
    """Layer 1: rebuild the 120-rule snapshot into a temp store; compare.

    `repo_root` is accepted for backward-compat but unused — the corpus comes
    from the pinned snapshot, not the live store.
    """
    with tempfile.TemporaryDirectory(prefix="reflect-conf-corpus-") as tmp:
        cwd, rules = build_store(tmp, _snapshot_specs())
        return _check(rules, fixtures.PARITY_QUERIES, cwd=cwd)


def run_synthetic_parity() -> list[QueryResult]:
    """Layer 2: hermetic temp store of crafted edge-case rules."""
    with tempfile.TemporaryDirectory(prefix="reflect-conf-synth-") as tmp:
        cwd, rules = build_store(tmp, fixtures.SYNTHETIC_RULES)
        return _check(rules, fixtures.SYNTHETIC_QUERIES, cwd=cwd)


def repo_root() -> str:
    # retained for callers; no longer used to source the corpus.
    return str(Path(__file__).resolve().parents[4])
