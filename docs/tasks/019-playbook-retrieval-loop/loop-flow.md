# Artifact: Retrieval Loop Flow (Task 019)

Command → event → projection flow. Events are canonical; everything right of the log is derived.

```
 AGENT (one session, scoped by session_id env / --session)
 │
 │ 1. auto reflect retrieve "add --json flag to auto env" [--domain go,cli]
 │      ◄── [{retrieval_id, use_when, domain, rule_type}]   (no content;
 │           domain-matched hard rules always present, hard_injected)
 │
 │ 2. auto reflect select rt-aa11 rt-bb22 rt-cc33     (most interesting first)
 │      ◄── [{feedback_id, content, causal_note, rule_type}]  (same order)
 │
 │ 3. ...does the coding task...
 │
 │ 4. auto reflect feedback '{json}'   (or '-' for stdin; --session to close orphans)
 │      { outcome: success|partial|fail|abandoned, summary,
 │        rankings: [{feedback_id, rank, reason}],   ← must cover ALL outstanding fb-ids
 │        gap: {report, moment} | null }             ← moment = grounding, required w/ gap
 │
 │ 5. auto reflect gate check          ← skills/hooks call this; exit 0 only when
 │                                       no outstanding feedback_ids in scope
 │                                       (no session detected → this host+worktree,
 │                                        24h lookback, --since override)
 ▼
┌─────────────────────────────────────────────────────────────────────┐
│ EVENT LOG (canonical, append-only, committed)                       │
│ .auto/reflect/events/<host>-<YYYY-MM-DD>-<wt8>.jsonl                │
│   (wt8 = sha256(worktree root)[:8] — parallel same-host worktrees   │
│    write different files, so merges never conflict on a path)       │
│                                                                     │
│ envelope: {id, type, schema_version, seq, ts, host, session_id?,    │
│            agent?, git:{hash, remote(sanitized)}, payload}          │
│                                                                     │
│ types: rule_created │ rule_edited(field deltas, version bump)       │
│        retrieval    │ selection(ordered, mints fb-ids) │ feedback   │
└──────────────┬──────────────────────────────────────────────────────┘
               │  Fold (deterministic order: ts, shard, seq;
               │        from_version mismatch → last-writer-wins,
               │        conflict reported by `rebuild`)
               ▼
┌──────────────────────────────┐      ┌────────────────────────────────┐
│ SNAPSHOT (derived, committed)│      │ READ SURFACES (derived, ad hoc)│
│ .auto/reflect/playbook.json  │      │ stats: surfaced/selected/      │
│ {schema_version,             │      │        selection_rate per rule │
│  folded_through:             │      │ gate check: selection fb-ids   │
│   {shard→last RULE seq},     │      │   minus feedback-covered ids   │
│  rules:[...]}                │      │   (session scope, else bounded │
│ stale only on rule events;   │      │    host+worktree+lookback)     │
│ `rebuild` forces refold      │      └────────────────────────────────┘
└──────────────────────────────┘

 RULE AUTHORING (same log, human/agent-driven)
   auto reflect rule create --use-when … --content … --causal-note …
                            --domain go,build --type soft        → rule_created
   auto reflect rule edit r-1a2b3c4d --content "…"               → rule_edited {field, v1→v2}
   auto reflect rule list / rule get r-1a2b3c4d                  ← reads snapshot
```

Deferred stages (probes, Phase-5 reviewer, triage, A/B, contrastive loop) will fold over this same
log — nothing here needs to change shape for them, they only add event types and readers.
