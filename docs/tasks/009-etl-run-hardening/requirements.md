# Task 009: ETL Run Hardening

## Problem

autoetl generates the source of truth for all downstream tools (search, reflect, skill). Today, session/message partitions are write-once: past partitions are skipped if the file already exists, writes go directly to the target path (no atomicity), and there is no backup, manifest, or merge path for old data. This means re-ingesting historical sessions (e.g. backfilling old Codex data) silently drops records, a crash mid-write corrupts a partition with no recovery, and there is no way to verify integrity of the output store.

As more clients and providers feed data into autoetl, the output store must be resilient to re-submissions, partial client data, crash recovery, and accidental destruction.

## Goals

- Enable safe merging of new data into any existing partition (past or current)
- Make all parquet writes atomic so a crash never leaves a corrupt partition
- Create per-partition manifests for integrity verification
- Back up partitions before modification so data is never silently lost
- Protect `--full` rebuilds with a pre-nuke snapshot
- Keep the backup interface abstract enough that an S3 backend can be added later (but only implement local-disk backup now)

## Acceptance Criteria

**AC-1**: Read-merge-write for session/message partitions
- Given: an existing `messages/year=2025/week=12/messages.parquet` with 500 messages
- When: `autoetl run` processes new raw data that includes 50 new messages and 10 messages with IDs already in that partition
- Then: the partition contains all 550 unique messages (existing wins on ID conflict), and 10 conflict warnings are logged to stderr

**AC-2**: Session latest-wins merge
- Given: an existing session row with `LastMessageAt=T1` and `MessageCount=100`
- When: incoming data contains the same session ID with `LastMessageAt=T2 > T1` and `MessageCount=120`
- Then: the merged session row has `LastMessageAt=T2` and `MessageCount=120` (latest-wins on temporal fields)

**AC-3**: Atomic parquet writes
- Given: any parquet write (messages, sessions, git, github)
- When: the write is performed
- Then: data is written to a temporary file in the same directory, then atomically renamed to the target path. If the process crashes mid-write, the previous partition file remains intact.

**AC-4**: Pre-write backup with rotation
- Given: an existing partition file that is about to be modified (merge or overwrite)
- When: the write begins
- Then: the existing file is copied to a backup directory (e.g. `~/.auto/etl/backup/messages/year=2025/week=12/messages.parquet.<timestamp>`) before the new version is written. Only the 3 most recent backups per partition are retained; older backups are pruned after a successful write.

**AC-5**: Per-partition manifest
- Given: a partition write completes successfully
- When: the parquet file is finalized
- Then: a sidecar manifest file (e.g. `messages.manifest.json`) is written alongside the parquet file, containing: row count, list of record IDs, SHA-256 hash of the parquet file, and write timestamp.

**AC-6**: Doctor manifest verification
- Given: `autoetl doctor` is run
- When: manifest files exist alongside parquet partitions
- Then: doctor verifies each parquet file's SHA-256 matches its manifest, reports mismatches as errors with remediation hints (e.g. "run autoetl run --full to rebuild"), and reports missing manifests as warnings.

**AC-7**: Full rebuild backup
- Given: `autoetl run --full` is invoked
- When: the output directory exists and contains data
- Then: the entire output store is snapshotted to the backup directory before deletion. The run then proceeds with a clean rebuild.

**AC-8**: Client data loss does not propagate
- Given: a client previously synced 1000 messages across 5 sessions
- When: the client re-syncs with only 800 messages (lost 200)
- Then: all 1000 original messages remain in the output store. The 200 "missing" messages are not deleted because merge is additive (existing wins, no deletions).

## Out of Scope

- S3 or remote backup implementation (design for it, don't build it)
- Compression of backup files
- Backup restore command (manual file copy for now)
- Changes to git/github writer merge logic (already uses read-merge-write)
- Multi-host merge conflicts (single-host assumption for now)
- Encryption of backup data

## Open Questions

- None; all resolved during requirements discussion.
