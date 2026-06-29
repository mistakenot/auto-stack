"""Synthetic rules + query batteries for the conformance harness.

The synthetic rules are crafted to exercise specific code paths in the matcher
(keyword scoring, domain filter, hard injection, score ties, lifecycle
exclusion). The parity queries are realistic intents run against the real
120-rule playbook to check broad Go/Python agreement.
"""
from __future__ import annotations

# Each synthetic rule: dict consumed by gocli.rule_create. `post` is an optional
# lifecycle transition applied after creation ("retire" -> stale, "promote" ->
# confirmed) to exercise surfaceable-lifecycle logic without relying on
# `rule create --lifecycle` accepting arbitrary values.
SYNTHETIC_RULES: list[dict] = [
    # keyword + domain scoring
    {"use_when": "writing go tests for etl parquet output",
     "content": "c", "causal_note": "n", "domain": ["go", "testing", "parquet"], "rule_type": "soft"},
    {"use_when": "writing go tests that parse json from a cli binary",
     "content": "c", "causal_note": "n", "domain": ["go", "testing", "cli"], "rule_type": "soft"},
    # hard rule, injects on domain even with zero keyword overlap
    {"use_when": "handling aws sigv4 request signing in go",
     "content": "c", "causal_note": "n", "domain": ["go", "aws", "security"], "rule_type": "hard"},
    # hard rule in a rare domain
    {"use_when": "normalizing a git remote url before broadcasting it",
     "content": "c", "causal_note": "n", "domain": ["git", "security"], "rule_type": "hard"},
    # two rules engineered to tie on score for a given query (same keywords hit)
    {"use_when": "running go build after editing a file",
     "content": "c", "causal_note": "n", "domain": ["go"], "rule_type": "soft"},
    {"use_when": "running go build before opening a pr",
     "content": "c", "causal_note": "n", "domain": ["go"], "rule_type": "soft"},
    # unrelated rule that should never surface for go/test queries
    {"use_when": "designing a frontend dashboard layout in the browser",
     "content": "c", "causal_note": "n", "domain": ["ui"], "rule_type": "soft"},
    # lifecycle: this one gets retired -> stale (never surfaces)
    {"use_when": "configuring a stale deprecated watch trigger",
     "content": "c", "causal_note": "n", "domain": ["watch"], "rule_type": "soft", "post": "retire"},
    # lifecycle: this one gets promoted -> confirmed (surfaces even with --no-drafts)
    {"use_when": "confirmed guidance for go module layout",
     "content": "c", "causal_note": "n", "domain": ["go", "monorepo"], "rule_type": "soft", "post": "promote"},
]

# (intent, domain or None, no_drafts) — exercises the synthetic store.
SYNTHETIC_QUERIES: list[tuple[str, list[str] | None, bool]] = [
    ("writing go tests", None, False),
    ("go tests parquet", None, False),
    ("json cli binary", None, False),
    ("aws signing", None, False),
    ("parquet", ["aws"], False),                 # hard aws injects despite no keyword
    ("go", ["testing"], False),                  # domain filter
    ("go", ["nonexistent-domain"], False),       # filter excludes everything scored; hard inject only on filter
    ("running go build", None, False),           # tie between two 'go build' rules
    ("git remote url", None, False),
    ("go module layout", None, False),
    ("go module layout", None, True),            # --no-drafts: only the confirmed rule
    ("designing dashboard", None, False),
    ("", None, False),                            # empty intent
    ("totally unrelated quantum biology", None, False),
]

# Realistic intents for broad parity against the live 120-rule playbook.
PARITY_QUERIES: list[tuple[str, list[str] | None, bool]] = [
    ("writing e2e tests that parse json output from a go cli", ["go", "cli"], False),
    ("creating a git worktree before opening a pr", ["git"], False),
    ("storing credentials in a config file", ["security"], False),
    ("normalizing git remote urls before logging", None, False),
    ("ast-grep typescript import scanning", ["graph"], False),
    ("writing tests for etl parquet output", ["etl", "parquet"], False),
    ("adding a cobra subcommand with flags", ["cli"], False),
    ("handling aws s3 sigv4 signing", None, False),
    ("deterministic ids for derived bus events", ["bus", "events"], False),
    ("reconnect loop with context timeout in autowatch", ["watch"], False),
    ("go test race on shared daemon state", ["go", "testing"], False),
    ("property based idempotence test for normalization", None, False),
    ("schema change in parquet columns", ["parquet"], False),
    ("registry filtering in an etl command", ["etl"], False),
    ("ui liveness conformance test bus event", ["ui"], False),
    ("pre-commit hook gofmt in a subagent workflow", ["hooks"], False),
    ("signal handling in a cobra serve command", ["cli", "go"], False),
    ("functional option for config loading in constructor", ["testing", "go"], False),
    ("merging a pr from a worktree", ["git"], False),
    ("go mod tidy in a workspace submodule", ["dependencies"], False),
    ("diagnostics to stderr not json output", ["cli", "graph"], False),
    ("unix socket stale file simulation in tests", ["networking"], False),
    ("planning doc references unmerged dependency", ["doc"], False),
    ("session intent prefix matching truncated preview", ["search"], False),
    ("checking acceptance criteria before closing a task", ["doc"], False),
    ("nothing matches this at all xyzzy", None, False),
    ("go", None, False),                          # near-universal keyword
    ("test", ["go"], False),
    ("writing go code", ["go"], False),
    ("e2e fixture real git repo", ["git", "testing"], False),
]
