---
hash: "724e5d2c"
id: "4763e61a"
read_when: "implementing durable hook event logging or the auto-etl hooks ingest pipeline"
summary: "Durable hook event log design: auto-shared/hooks envelope format, append-first producer, incremental ETL consumer with watermark sync-state, and monthly merge-by-ID parquet writer."
title: "Solution: Task 022 — Hook Event Log"
---

# Solution: Task 022

## Approach

The producer (`auto hooks fire`) and consumer (`auto etl run`) share only a
**file format**, never code at runtime — consistent with the monorepo rule "the
tool binaries don't depend on each other; they depend on a common data / file
format." That format is owned by a small new `auto-shared/hooks` package that
both binaries import for the envelope type + path layout (auto-shared is already
the shared layer both modules depend on).

The consumer reuses the **git ETL source pattern** end-to-end: JSON sync-state
watermark (`internal/git/state.go`), `readExistingParquet`/`mergeByID` generic
writer helpers (`internal/writer/git.go`), and the `runXxxETL` phase wiring in
`cmd/run.go`. Hooks are ingested **incrementally** (only new bytes per file) and
written with **merge-by-ID** so a crash between "write parquet" and "save
watermark" re-reads but never duplicates.

1. **Define the file-format contract** — `auto-shared/hooks`: an `Envelope`
   struct (thin capture wrapper: agent, captured-at, host id, **cwd**,
   **project**, verbatim payload), the daily log path layout, an
   append-one-line helper, and the **shared normalization extractors**
   (`ExtractEventName`/`ExtractSessionID`/`ExtractTool`/`ExtractPaths`) that both
   the producer's `buildHookEvent` and the ETL consumer call — one
   implementation, no drift.
   - *What the envelope captures vs. what ETL derives:* the envelope stores only
     the fields the raw payload **can't reliably supply at ETL time** — agent,
     timestamp, host id, and the two host/registry-relative resolutions the
     producer alone can do correctly: `cwd` (`buildHookEvent` falls back to
     `os.Getwd()` when the payload omits it, hookscmd.go:109-111) and `project`
     (resolved against the *producer's* local `~/.auto/projects.json`,
     hookscmd.go:115-119). Both are producer-authoritative because raw data "can
     be from multiple hosts" (CLAUDE.md) so the ETL host's cwd and registry may
     differ — re-resolving either at ETL time risks a wrong/empty value. Fields
     derivable purely from the verbatim payload — event name, session id, tool,
     paths — are left to ETL, which re-extracts them via the shared
     `auto-shared/hooks` extractors. `LogPath` is always fed `time.Now().UTC()`
     so the filename day matches the UTC `CapturedAt` day (no local/UTC split at
     a midnight boundary, which would otherwise put the parquet month partition
     and the raw filename on different days).

<!-- RESOLVED(P2): Project resolution rationale contradicts the cwd-in-envelope argument
REVIEW: This item justifies putting cwd in the envelope because "If ETL re-derived cwd
later it would read the *ETL host's* working dir" — i.e. the producer is the authoritative
resolver because the ETL host differs from the producer host. But the very same argument
applies to `project`, which you defer to ETL ("project ... is normalized at ETL time"):
ETL resolves it via `registry.FindProjectByPath(cwd)` against the *ETL host's*
`~/.auto/projects.json` (verified: solution item 5, context.md:36-39). In a multi-host
setup (CLAUDE.md: raw "can be from multiple hosts") the ETL host's registry may not contain
the producer's project, so the resolved `project` column can be wrong/empty — exactly the
failure mode you cite for cwd. The producer ALREADY resolves project today
(hookscmd.go:115-119) with the correct local registry. Either (a) store the resolved
project id in the envelope too (single authoritative resolver, consistent with the cwd
reasoning), or (b) explicitly document that `project` is best-effort ETL-host resolution
and acceptable because v1 is single-host. As written the reasoning is internally
inconsistent.
AUTHOR: Took option (a). The reasoning was inconsistent; `project` has the identical
multi-host hazard as `cwd`. Item 1 now stores producer-resolved `project` in the envelope
alongside `cwd` (single authoritative resolver), the `Envelope` struct gains a `Project`
field, item 5 copies `project` straight from the envelope instead of re-resolving it, and
the `HookEventRow` comment now reads "copied from envelope (producer-resolved)". ETL only
derives the fields that are purely payload-internal (event/session/tool/paths).
-->

2. **Producer: durable append in `auto hooks fire`** — durable capture is the
   primary job, so append **first**, then `postHookEvent` (the POST is the
   lossy/optional live signal; capturing to disk before notifying guarantees the
   canonical record even if the process is later killed). Append one `Envelope`
   line (the verbatim stdin bytes as `payload`) to
   `~/.auto/hooks/raw/events-YYYY-MM-DD.jsonl`. Load host id quietly (mirrors
   ETL's `loadHostID`, falls back to `os.Hostname`) — one stat+read of
   `~/.auto/host.json` per fire, the same tiny read ETL does and negligible
   against the 150ms POST budget; accepted as-is, not cached. Any failure →
   stderr + continue; the command still exits 0 and still POSTs. The append is a
   single `os.OpenFile(…, O_APPEND|O_CREATE|O_WRONLY)` + a single `f.Write` of
   the pre-marshaled line+`\n` — **not** a `bufio.Writer` that could flush in
   chunks. The interleave guarantee is the Linux one: a single `write()` to a
   regular file holds the inode lock for the whole call, so concurrent agents'
   lines don't interleave even at the ~1 MiB payload bound (POSIX's 4 KiB
   `PIPE_BUF` atomicity rule covers only pipes and is not the relevant guarantee
   here). The append has **no explicit timeout**: it is local-disk I/O assumed
   sub-millisecond; a pathological contended/NFS home dir could block the hot
   path, a known and accepted v1 limitation (bounding a local append would mean
   spawning a goroutine for marginal benefit, and the POST that follows is
   already time-bounded at 150ms).

<!-- RESOLVED(P3): "atomic for small writes" overstates the guarantee given 1 MiB payloads
REVIEW: The interleave-safe O_APPEND guarantee POSIX makes for concurrent writers is only
firm up to PIPE_BUF (4096 bytes). The producer bounds stdin at maxHookPayloadBytes = 1 MiB
(hookscmd.go:30), and the envelope wraps that verbatim payload, so a single appended line
can be ~1 MiB — well over "small". On Linux/ext4 a single write() of a regular file is in
practice atomic w.r.t. other writers (inode lock held for the whole write), so this is
likely fine, but the justification as written ("atomic for small writes on POSIX") is the
wrong reason. Either restate the guarantee as Linux-specific single-write()-holds-inode-lock,
or note the large-payload case explicitly. (Also ensure it really is ONE Write call, not a
buffered writer that can flush in chunks.)
AUTHOR: Restated in item 2. The guarantee is now "a single write() to a regular file holds
the inode lock for the whole call" (valid at the ~1 MiB bound), with the note that POSIX's
4 KiB PIPE_BUF rule covers only pipes and is not the relevant guarantee. Item 2 also now
pins the implementation to a single `os.OpenFile(O_APPEND|O_CREATE|O_WRONLY)` + single
`f.Write`, explicitly NOT a `bufio.Writer` that could flush in chunks.
-->

<!-- RESOLVED(P3): Durable append has no time bound, unlike the 150ms POST it sits beside
REVIEW: AC-1/AC-2 require staying "within the existing hot-path budget", and the existing
budget is the explicit hookPostTimeout = 150ms on the POST (hookscmd.go:22, context.md:21).
The synchronous append has error-swallowing but NO timeout/deadline — a slow or contended
disk (NFS home dir, fsync storm) blocks the agent's critical path with no upper bound. The
POST is protected; the append is not. Either acknowledge that local file append is assumed
fast enough (and say so), or bound it. Relatedly: item 2 says append runs "before/independent
of postHookEvent" — if it runs before and blocks, it also delays the live POST. Consider
documenting the ordering rationale (POST first for liveness, then durable append?).
AUTHOR: Item 2 now states the append is unbounded local-disk I/O assumed sub-millisecond,
and that a pathological contended/NFS home dir blocking the hot path is a known, accepted
v1 limitation (an explicit acknowledgement, not a silent gap). On ordering I chose the
opposite of the suggestion and documented why: append **first**, then POST. Durable capture
is the entire point of this task and is canonical, while the POST is lossy/optional — so we
guarantee the disk record before the live notify, accepting a sub-ms delay to the POST. The
POST that follows remains bounded at 150ms.
-->

3. **Consumer parquet model** — `auto-etl/internal/model/hooks.go`:
   `HookEventRow` with normalized dict columns + `raw_json` + provenance, keyed
   by a position-stable `id`.

4. **Consumer sync-state** — `auto-etl/internal/hooks/state.go`:
   `HooksSyncState` mapping each log filename → consumed byte offset; load/save
   atomic, lenient-on-corrupt — a direct analog of `git/state.go`. Path:
   `~/.auto/etl/hooks/sync-state.json`.

5. **Consumer ingest** — `auto-etl/internal/hooks/ingest.go`: list
   `events-*.jsonl` sorted; for each, skip if `offset >= size`, else seek to the
   watermark and read **whole lines only** with `bufio.Reader.ReadBytes('\n')` —
   **not** a default `bufio.Scanner`, whose 64 KiB token cap would error/drop the
   ~1 MiB envelope lines the 1 MiB payload bound allows (`ReadBytes` has no cap;
   cf. the in-repo `scanner.Buffer(1MiB, 1MiB)` workaround at parser.go:206-207,
   which would itself be marginally too small once the envelope wraps a 1 MiB
   payload). A trailing chunk returned with `io.EOF` and no `\n` is an in-flight
   partial append: discard it and do **not** advance the offset past it. Parse
   each `Envelope` leniently into a `HookEventRow` (malformed payload → row with
   `raw_json` + whatever the shared extractors recovered, never aborts): copy
   `agent`/`cwd`/`project`/`host` straight from the envelope, and derive
   `event`/`session`/`tool`/`paths` from the verbatim payload via the shared
   `auto-shared/hooks` extractors. The row `id` and the `host_id` column both use
   the **envelope's** `HostID` (producer-stamped, never empty since
   `hostIDQuietly` always returns something) so a given (file, offset) line
   yields the *same* `id` no matter which host ingests it — host-stable ids keep
   the merge idempotent even when raw logs from multiple hosts land in one shared
   dataset. `Ingest` therefore needs neither the registry (project comes from the
   envelope) nor the ETL-process host id except as an empty-envelope fallback;
   its signature is `Ingest(rawDir string, state *HooksSyncState, fallbackHostID
   string)`. Return rows + advanced offsets.

<!-- RESOLVED(P2): "scan whole lines only" with a default bufio.Scanner will break on 1 MiB lines
REVIEW: Envelope lines can be ~1 MiB (payload bound = maxHookPayloadBytes 1 MiB, hookscmd.go:30,
plus envelope overhead). A default `bufio.Scanner` caps tokens at 64 KB and returns
bufio.ErrTooLong on longer lines — which would either drop large hook events (PostToolUse with
big tool_input dominates volume, context.md:114) or abort the run, violating AC-6 (lenient).
There is an in-repo precedent for exactly this: parser.go:206-207 does
`scanner.Buffer(make([]byte, 1024*1024), 1024*1024)`. Note even a 1 MiB buffer is marginally
too small once the envelope wraps a 1 MiB payload — size the buffer to maxHookPayloadBytes +
envelope overhead, or use bufio.Reader.ReadBytes('\n'). Please call out the scanner-buffer
sizing in the ingest design so the implementer doesn't reach for the default Scanner.
AUTHOR: Item 5 now mandates `bufio.Reader.ReadBytes('\n')` (no token cap) and explicitly
calls out that a default `bufio.Scanner` would error on the ~1 MiB lines, citing the
parser.go:206-207 precedent and noting even its 1 MiB buffer is marginally too small once the
envelope wraps a 1 MiB payload. `ReadBytes` also makes the partial-trailing-line handling
exact: a final chunk with `io.EOF` and no `\n` is discarded and the offset is not advanced.
-->

6. **Consumer writer** — `auto-etl/internal/writer/hooks.go`: `WriteHooks`
   groups rows by **month** and, per partition, `readExistingParquet` →
   `mergeByID` (incoming wins) → `writeParquet`. Monthly + merge mirrors the git
   datasets exactly; daily *raw* files already provide rotation, so the parquet
   layer need not also be daily.

7. **Consumer wiring** — `auto-etl/cmd/run.go`: add `"hooks"` to
   `validOnlyValues`, the default-all map, and the `--only` help text; add a
   `runHooksETL(hostID)` phase that loads state, ingests, writes parquet, **then**
   saves state.

## Files

```
+ auto-shared/hooks/log.go              # Envelope; RawDir/LogPath(t); Append; Extract* normalizers
+ auto-shared/hooks/log_test.go         # append round-trip, path layout, Extract* cases
~ auto-cli/cmd/auto/hookscmd.go         # fire: append envelope (incl. cwd+project) first; hostIDQuietly(); buildHookEvent/extractPaths delegate to hooks.Extract*
~ auto-cli/cmd/auto/hookscmd_test.go    # log-written; failure-swallowed-still-exits-0
+ auto-etl/internal/model/hooks.go      # HookEventRow parquet struct
+ auto-etl/internal/hooks/state.go      # HooksSyncState (per-file byte offset)
+ auto-etl/internal/hooks/state_test.go # save/load round-trip, corrupt-tolerant
+ auto-etl/internal/hooks/ingest.go     # discover + seek-from-offset + parse → rows
+ auto-etl/internal/hooks/ingest_test.go# incremental, lenient, partial-line handling
+ auto-etl/internal/writer/hooks.go     # WriteHooks: monthly merge-by-ID
~ auto-etl/cmd/run.go                    # validOnlyValues + runHooksETL + help text
~ auto-etl/cmd/run_only_test.go          # parseOnlyFlag accepts/normalizes "hooks" (existing --only tests)
```

### Key outlines

```go
// auto-shared/hooks/log.go
type Envelope struct {
    Agent      string          `json:"agent"`             // "claude" | "codex"
    CapturedAt string          `json:"capturedAt"`        // RFC3339 UTC
    HostID     string          `json:"hostId"`
    Cwd        string          `json:"cwd,omitempty"`     // producer-resolved (Getwd fallback)
    Project    string          `json:"project,omitempty"` // producer-resolved (local registry)
    Payload    json.RawMessage `json:"payload"`           // verbatim hook stdin
}
func RawDir() (string, error)              // ~/.auto/hooks/raw
func LogPath(t time.Time) (string, error)  // .../events-2006-01-02.jsonl; caller passes time.Now().UTC()
func Append(env Envelope) error            // OpenFile(O_APPEND|O_CREATE|O_WRONLY) + one Write of line+\n

// Shared normalization — one implementation used by BOTH the producer's
// buildHookEvent and the ETL ingest, so the two cannot drift.
func ExtractEventName(payload map[string]any) string // hook_event_name
func ExtractSessionID(payload map[string]any) string // session_id
func ExtractTool(payload map[string]any) string      // tool_name
func ExtractPaths(payload map[string]any) []string   // file_path/notebook_path/path, sorted+deduped
```

<!-- RESOLVED(P3): Pin the day-boundary timezone — filename date vs CapturedAt must agree
REVIEW: `CapturedAt` is specified as "RFC3339 UTC", but `LogPath(t)` takes a caller-supplied
`t` whose timezone is unstated. If the producer passes a local-time `t` for the filename while
stamping CapturedAt in UTC, an event near midnight lands in `events-2026-06-11.jsonl` with a
capturedAt of `2026-06-12T...Z` (or vice-versa). ETL partitions parquet by CapturedAt month
(item 6), so the raw filename and the parquet partition can disagree at boundaries, and AC-1's
"that day's log file" becomes ambiguous. Specify that `LogPath` is fed `time.Now().UTC()` so
the filename day and CapturedAt day are the same clock. Harmless to data integrity, but worth
pinning so the implementer doesn't mix local and UTC.
AUTHOR: Pinned. Item 1 now states `LogPath` is always fed `time.Now().UTC()` so the filename
day equals the UTC `CapturedAt` day (no midnight local/UTC split between the raw filename and
the parquet month partition), and the `LogPath` signature comment says "caller passes
time.Now().UTC()".
-->

<!-- RESOLVED(P3): hostIDQuietly adds a per-fire file read to the hot path — confirm it's acceptable/cached
REVIEW: The Files table lists `hostIDQuietly()` in hookscmd.go, and solution item 2 says the
producer loads host id on every fire (read of ~/.auto/host.json). That's an extra stat+read on
the agent's critical path for every single hook (PostToolUse fires constantly). Almost certainly
negligible vs the 150ms POST budget, but the solution should state it's accepted (or note it's a
tiny read) rather than leave a second piece of unbounded hot-path I/O implicit alongside the
durable append.
AUTHOR: Made explicit in item 2: the host-id load is "one stat+read of ~/.auto/host.json per
fire, the same tiny read ETL does and negligible against the 150ms POST budget; accepted
as-is, not cached." The producer already does the comparable registry read for project
resolution today, so this adds one more small fixed-size read, not unbounded work.
-->


```go
// auto-etl/internal/model/hooks.go
type HookEventRow struct {
    ID         string `parquet:"id"`               // sha256(envelope.HostID\x00file\x00offset) — host-stable
    HostID     string `parquet:"host_id,dict"`
    Agent      string `parquet:"agent,dict"`
    Event      string `parquet:"event,dict"`       // hook_event_name
    SessionID  string `parquet:"session_id,dict"`
    Cwd        string `parquet:"cwd"`
    Project    string `parquet:"project,dict"`     // copied from envelope (producer-resolved)
    Tool       string `parquet:"tool,dict"`        // tool_name
    PathsJSON  string `parquet:"paths_json"`       // JSON array, "" if none
    CapturedAt int64  `parquet:"captured_at"`      // Unix ms
    RawJSON    string `parquet:"raw_json"`         // verbatim payload
    SourceFile string `parquet:"source_file,dict"` // events-YYYY-MM-DD.jsonl
    Year          int32 `parquet:"year"`
    Month         int32 `parquet:"month"`
    SchemaVersion int32 `parquet:"schema_version"`
}
```

<!-- RESOLVED(P3): Row ID's host source contradicts the HostID column's host source
REVIEW: The `id` is defined as `sha256(hostID\x00file\x00offset)` and plan Step 3.4 feeds it the
ETL-process hostID (`loadHostID()`). But item 5 says the `host_id` column is "copied from envelope
(producer-resolved)" — the producer's stamped HostID. So within one row, the host baked into `id`
(ETL-local) and the `host_id` column (envelope) can disagree. On a single host they're identical
and this is harmless. But the whole reason cwd/project/host live in the envelope (your RESOLVED(P2)
rationale, and CLAUDE.md "raw can be from multiple hosts" / auto-etl's shared-bucket aspiration) is
that the ETL host may differ from the producer host. Under that future, the same immutable
(file,offset) line ingested on two hosts yields two different ids → duplicate rows in a shared
parquet dataset, weakening the crash-idempotency the offset+id scheme is designed to give. For
host-stable ids, derive BOTH the `id` prefix and the `host_id` column from the envelope's HostID
(which the producer always sets via hostIDQuietly, never empty), making the row fully
self-describing. The `hostID` param to Ingest then becomes only an empty-envelope fallback (and,
with the registry param already removable, Ingest's signature shrinks toward `(rawDir, state)`).
AUTHOR: Adopted. Both the `id` prefix and the `host_id` column now derive from the **envelope's**
HostID (item 5 + the `HookEventRow.ID` comment updated to `sha256(envelope.HostID\x00file\x00offset)`),
so a (file,offset) line is host-stable and re-ingest on any host dedups via mergeByID. `Ingest`'s
signature drops the registry entirely and keeps the ETL host id only as `fallbackHostID` for the
rare empty-envelope line: `Ingest(rawDir, state, fallbackHostID)`. plan Step 3.4 + 4.2 updated to
match (registry load removed from runHooksETL).
-->

```go
// auto-etl/internal/hooks/state.go
type HooksSyncState struct {
    SchemaVersion int                   `json:"schema_version"`
    Files         map[string]*FileState `json:"files"` // key: log filename
}
type FileState struct{ Offset int64 `json:"offset"` }
func HooksSyncStatePath() string                      // ~/.auto/etl/hooks/sync-state.json
func LoadHooksSyncState(path string) *HooksSyncState  // lenient
func (s *HooksSyncState) Save(path string) error      // atomic temp+rename
```

## Test Coverage

| AC   | Test Type   | File                                            |
|------|-------------|-------------------------------------------------|
| AC-1 | unit + integ| `auto-shared/hooks/log_test.go`, `auto-cli/cmd/auto/hookscmd_test.go` (pipe payload → assert line appended verbatim + envelope) |
| AC-2 | unit        | `auto-cli/cmd/auto/hookscmd_test.go` (unwritable raw dir → exit 0, POST still fires) |
| AC-3 | integration | `auto-etl/internal/hooks/ingest_test.go` + `auto-etl/internal/writer/hooks.go` via a writer/ingest round-trip reading back parquet columns incl. `raw_json` |
| AC-4 | unit + integ| `auto-etl/internal/hooks/state_test.go` (watermark round-trip); `ingest_test.go` (run twice → no dup rows; append line → only new ingested) |
| AC-5 | unit        | `auto-etl/cmd/run_only_test.go` (`parseOnlyFlag` accepts `hooks`, default includes it, invalid still rejected) |
| AC-6 | unit        | `auto-etl/internal/hooks/ingest_test.go` (malformed/garbage line → row with `raw_json` intact, run not aborted) |

## Out of Scope

- Changing the live UI POST path or the `HookEvent` wire shape (task 021 owns the
  bus envelope; this task is the durable/offline sink).
- auto-search indexing of the `hooks` dataset and auto-reflect consumption.
- Log rotation/retention/compaction beyond daily file naming; the log is
  append-only and immutable, pruning is later.
- Backfilling hook events from existing transcripts (capture is forward-only).
- `auto hooks install` changes / the telemetry-safe event allowlist (task 020).
- Configurable log path — fixed at `~/.auto/hooks/raw/` for v1.
- Parquet-list typing of `paths` (stored as a JSON string, matching the
  `*_json` column convention in the PR/git models).

## Rejected Alternatives

- **Define the envelope struct + normalization helpers separately in each binary
  (no shared package)**: strictly honors "tools depend on file format not code,"
  but risks the stable wrapper fields *and* the field/path extraction logic
  drifting between producer and consumer. Note the existing extractors
  (`extractPaths`/`stringField`/`buildHookEvent`) live in auto-cli's `package
  main`, which auto-etl cannot import — so without a shared package the consumer
  would *re-implement* them. Chose a shared `auto-shared/hooks` contract that
  owns the `Envelope` type **and** the `Extract*` normalizers — auto-shared
  already plays exactly this role, and both binaries call one implementation.
- **Store task 021's CloudEvents bus envelope in the log**: couples this task to
  021 landing first and ties the durable record to a still-evolving live wire
  format. Rejected per requirements Q4 (fully independent).
- **Per-line content-hash dedup instead of a byte-offset watermark**: requires
  reading the whole existing dataset to dedup and risks dropping legitimately
  identical events (e.g. two identical `SessionStart`). Chose the offset
  watermark (Q3); a *position*-based row `id` (file+offset) then gives crash-safe
  idempotency without content collisions.
- **Daily parquet partitions**: more, smaller files and a deviation from every
  existing dataset. Monthly + merge matches git/sessions and reuses
  `mergeByID`; the daily *raw* files already provide rotation.
- **Full-regen of the current partition (the messages/sessions writer style)**:
  wrong for a watermark-incremental source that only sees new lines — it would
  drop earlier rows in the partition. Use git-style read-merge-write instead.
- **Write the log from the auto-ui server rather than the producer**: the UI is
  the component most likely to be down — the exact loss this task fixes. The
  producer is the only guaranteed capture point.
