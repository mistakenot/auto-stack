# Stage 2 · Consolidate — observations → gated rules, then curate

Turn clusters of observations into **draft rules**, then move each through its
lifecycle. `consolidate` is deterministic and gated: it validates, enforces
evidence thresholds, dedupes against live rules, flags conflicts, and only then
persists. Always preview with `--dry-run` first.

## Lifecycle

```text
  draft ──promote (≥3 tasks OR ≥2 sessions)──▶ confirmed ──graduate (linter+check)──▶ enforced
    │                                              │                                     │
    └──────────────────── retire (any → stale, never surfaced again) ───────────────────┘
```

- **draft** — candidate; surfaced in `retrieve` but flagged `draft: true`.
- **confirmed** — earned its place; surfaced normally.
- **enforced** — a linter now covers it, so it's **excluded from retrieve**.
- **stale** — retired; never surfaced.

## Apply a consolidation delta document

`consolidate` takes the JSON document as a positional argument **or `-` for
stdin** (it is *not* a file path — `consolidate file.json` tries to parse the
filename as JSON). Use a stdin heredoc:

```bash
auto reflect consolidate - --dry-run <<'JSON'
{ "deltas": [
  { "op": "create-draft",
    "use_when": "editing go files",
    "content": "After modifying a Go file, run go build ./... from that file's module dir",
    "causal_note": "unbuilt files hide compile errors that surface later as a batch",
    "domain": ["go"], "type": "soft",
    "observation_ids": ["ob-1a2b3c4d", "ob-5e6f7a8b"] }
] }
JSON
```

Drop `--dry-run` to apply. Output is `{ applied, skipped (with reasons), conflicts }`,
plus a top-level `.ids` (every applied rule id, in order) and `.id` (the first) so
you can thread minted ids without walking `applied[]`:
`auto reflect consolidate - <<<"$DOC" | jq -r '.ids[]'`. Because rule ids are now
**content-derived**, `--dry-run` yields the same ids as the apply, and re-running an
identical consolidate is **idempotent** (the existing rule wins). Unknown ops/fields
fail fast (the decoder rejects unknown fields).

### Ops

| Op | Required | Effect |
|---|---|---|
| `create-draft` | `use_when`, `content`, `causal_note` | mint a draft rule (gated, see below) |
| `attach-evidence` | `rule_id`, `observation_ids` | add evidence to an existing rule |
| `merge` | `rule_ids` (+ optional `into_use_when`, `reason`) | fold several rules into one |
| `deprecate` | `rule_id` (+ optional `reason`) | retire a rule to stale |
| `split` | `rule_id`, `into[]` | retire one too-broad rule into narrower drafts |

### Gates and guards (don't fight them)

- **create-draft evidence gate** — needs `observation_ids` spanning **≥2 distinct
  sessions**. Escapes: a `high`-severity (incident) observation auto-bypasses, or
  `--force`. If you `--force`, say so in your summary to the user.
- **Dedup** — a `use_when` that strongly overlaps an existing live rule is refused;
  use `attach-evidence` against that rule instead of minting a near-duplicate.
- **Conflicts** — possibly-contradictory rules are reported in `conflicts`; review
  them, they're not auto-resolved.

### split — narrows one rule into many, with two-way lineage

```bash
auto reflect consolidate - <<'JSON'
{ "deltas": [
  { "op": "split", "rule_id": "r-1a2b3c4d", "reason": "too broad",
    "into": [
      { "use_when": "editing go files", "content": "run go build ./... from the module dir",
        "causal_note": "module-scoped builds catch errors fast", "domain": ["go"] },
      { "use_when": "editing go tests", "content": "run go test ./... from the module dir",
        "causal_note": "split off the test-specific guidance", "domain": ["go"] }
    ] }
] }
JSON
```

The parent goes **stale** with `successor_ids = [children]`; each child is a new
**draft** with `predecessor_ids = [parent]`. Two children with an identical
`use_when` + `domain` in the same batch are rejected — give each a distinct
`use_when`.

## Curate the lifecycle directly

```bash
auto reflect rule promote <r-id>   # draft → confirmed; needs ≥3 distinct tasks OR ≥2 distinct
                                   # evidence sessions in provenance (--force overrides)
auto reflect rule retire  <r-id>   # any → stale (always allowed)
auto reflect rule list --lifecycle draft     # filter; omit --lifecycle to list all
auto reflect rule get <r-id>                 # full rule incl. provenance + lineage
auto reflect rule edit <r-id> --content "…"  # all changed fields = ONE versioned edit
```

### Graduate a confirmed rule into a static check

When a linter can enforce a rule deterministically, graduate it — the rule stays
true but stops riding along in retrieval.

```bash
auto reflect rule graduate <r-id> --linter golangci-lint --check errcheck \
  --config-path .golangci.yml --commit "$(git rev-parse --short HEAD)" \
  --note "now enforced statically"
#   sets lifecycle=enforced and records a lint_ref {linter, check, config_path, commit, note}
#   --force to graduate even a stale rule
```

**You cannot set `enforced` directly** — `rule create/edit --lifecycle enforced`
is rejected (`enforced` requires a `lint_ref`). Graduation is the only path, so
every enforced rule carries provenance for *which* check replaced it.

```bash
auto reflect rule list --lifecycle enforced   # the graduated set (excluded from retrieve)
```

## Optional: author a rule directly

Consolidation is the usual path, but you can hand-author one:

```bash
auto reflect rule create \
  --use-when "writing flaky end-to-end tests" \
  --content "Keep passing test logs short so failing E2E tests are easy to debug" \
  --causal-note "noisy passing logs hid the real failure during a debug session" \
  --domain testing --type soft          # --causal-note is required; --lifecycle defaults to draft
# rule create/edit/promote/retire/graduate all expose the rule id at top-level .id:
#   RID=$(auto reflect rule create ... | jq -r '.id')
# and `rule list` rows carry .id plus created_at/updated_at metadata.
auto reflect rebuild                     # force a refold of playbook.json (it's a disposable cache)
```

## Rules

- Always `--dry-run` first; read `skipped[].reason` before applying.
- Satisfy a gate with evidence, not by reflexively reaching for `--force`; if you
  do force, disclose it.
- Prefer `attach-evidence` over minting a near-duplicate the dedup gate will block.
- `causal_note` is mandatory on every rule — capture the failure it prevents.

Next: **`references/retrieve.md`** to put rules to work on a task.
