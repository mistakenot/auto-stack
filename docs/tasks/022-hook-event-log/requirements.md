---
hash: "67af6926"
id: "ac16a222"
read_when: "implementing the hook event log or understanding the durable capture and ETL ingestion requirements for agent hook payloads"
summary: "Requirements for adding a durable append-only JSONL hook event log to auto hooks fire, plus an auto-etl hooks source that ingests the log into a normalized parquet dataset with raw_json preservation."
title: "Requirements: Hook Event Log (Task 022)"
---

# Task 022: hook-event-log

## Problem

`auto hooks fire` (task 020) is the single point every Claude/Codex hook flows
through, but the only thing it does with a payload is best-effort POST a small
normalized `HookEvent` to auto-ui — a live, explicitly-lossy signal. If the UI is
down (the common case), the event is gone. There is no durable record of hook
activity for auto-etl to ingest, so the rich, high-frequency hook stream (every
tool call, prompt, session start/stop across every agent) is never captured into
the canonical parquet datasets that auto-search and auto-reflect build on.

## Goals

- **Durable capture**: `auto hooks fire` appends every payload it receives to an
  append-only JSONL log under `~/.auto/`, as an immutable record, *in addition
  to* (and independent of) the best-effort UI POST. Writing the log must never
  break the agent: it stays within the hot-path budget and any failure is
  swallowed (exit 0), exactly like the POST.
- **Lossless on disk**: each JSONL line preserves the agent's *original* hook
  payload **verbatim** (no fidelity loss), wrapped in a thin capture envelope
  carrying only what the raw payload can't reliably supply: the agent
  (`claude`/`codex`), a capture timestamp, and host id. Normalization is the
  ETL's job, not the producer's — the log is the canonical raw record.
- **ETL ingestion**: `auto etl run` learns a new `hooks` source that reads the
  JSONL log(s) and writes a new partitioned parquet dataset (`hooks`) alongside
  `messages` and `sessions`. Ingestion is incremental (re-running does not
  re-ingest or duplicate already-processed events) and non-destructive (never
  mutates the raw JSONL).
- **Normalize-and-preserve schema**: the `hooks` parquet table pulls a small,
  stable set of normalized columns out of the payload — capture timestamp, agent,
  hook event name, session id, cwd, resolved project id, tool name, paths[],
  host id — **plus a `raw_json` column holding the full original payload**. This
  keeps the table queryable while surviving the fact that per-agent/per-tool hook
  schemas change frequently over time.

## Acceptance Criteria

**AC-1**: Durable hook log written on every fire
- Given: a hook payload piped to `auto hooks fire --agent claude` (or `codex`)
- When: the command runs, regardless of whether auto-ui is reachable
- Then: a new line is appended to that day's log file
  (`~/.auto/hooks/raw/events-YYYY-MM-DD.jsonl`, created if absent); the line
  contains the original payload verbatim plus a capture envelope (agent, capture
  timestamp, host id); the command still exits 0 and never blocks beyond the
  existing hot-path budget

**AC-2**: Log failures never disrupt the agent
- Given: the log path is unwritable (e.g. directory missing/permission denied)
- When: `auto hooks fire` runs
- Then: the error is swallowed (written to stderr at most), the command exits 0,
  and the UI POST still happens

**AC-3**: New `hooks` ETL source and parquet dataset
- Given: a hook event log with N lines
- When: `auto etl run --only hooks` is invoked
- Then: a `hooks` parquet dataset is written under the ETL output dir, time
  partitioned, with one row per log line; each row has the normalized columns
  (capture ts, agent, event name, session id, cwd, project, tool, paths[], host)
  and a `raw_json` column containing the verbatim original payload

**AC-4**: Incremental, idempotent ingestion
- Given: `auto etl run --only hooks` has already ingested the current log, with a
  per-file byte-offset watermark persisted in the ETL sync-state
- When: it is run again
- Then: files fully consumed are skipped and the current day's file resumes from
  its watermark; with no new lines, no duplicate rows are produced; when lines
  have been appended since, only the new (post-watermark) lines are ingested

**AC-5**: `--only` validation includes `hooks`
- Given: the `auto etl run --only` flag
- When: `--only hooks` is passed (or `hooks` combined with other sources)
- Then: it validates and runs the hooks source; the default (no `--only`) run
  includes hooks alongside sessions/github/git; help text lists `hooks`

**AC-6**: Lenient parsing
- Given: a log line whose raw payload is malformed or has an unexpected/evolved
  shape
- When: the hooks source ingests it
- Then: the row is still written with `raw_json` intact and whatever normalized
  fields could be extracted; ingestion does not abort the run

## Out of Scope

- Changing the live UI POST path or the `HookEvent` wire shape (task 021,
  auto-bus-standard, owns the bus envelope; this task is the durable/offline
  sink, not the live event loop).
- auto-search indexing of the new `hooks` dataset, and auto-reflect consuming it
  (follow-on tasks once the table exists).
- Log rotation/retention/compaction policy beyond the file-layout decision below
  (the log is append-only and immutable; pruning is a later concern).
- Backfilling hook events from existing transcripts — the log only captures from
  install forward.
- `auto hooks install` changes (task 020 already wires fire onto the events).

## Open Questions

- [x] Q1: One task or split? (answered: **One task** — producer JSONL sink + ETL
      `hooks` dataset ship together, coupled by the JSONL line contract and
      verifiable end-to-end fire → log → parquet.)
- [x] Q2: Log layout on disk? (answered: **Daily-partitioned files** —
      `~/.auto/hooks/raw/events-YYYY-MM-DD.jsonl`; the producer appends to the
      current day's file. Bounded sizes, natural rotation, ETL can skip fully
      consumed files. Mirrors the etl `raw/` partitioning style.)
- [x] Q3: Incremental ingestion tracking? (answered: **File + byte-offset
      watermark** — ETL persists a per-file last-consumed offset / completed-file
      set in its sync-state (like the existing git sync state). Relies on the log
      being strictly append-only/immutable, which it is.)
- [x] Q4: Relationship to task 021's bus? (answered: **Fully independent** — the
      log stores raw payload + thin capture envelope, decoupled from 021. The bus
      is the live/lossy path; the log is the durable/offline path. No sequencing
      dependency — this task can land first or in parallel.)
