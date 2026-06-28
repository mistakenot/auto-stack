# Feedback: Task 051

## Problems faced
1. **Hand-rolled SigV4 had no in-repo precedent** — the whole signer/canonical-request/
   signing-key chain had to be built and proven from scratch. Anchored it on AWS's
   published `get-vanilla` test vector first, then validated with a real live PutObject
   before building anything on top.
2. **Wire path vs. canonical URI mismatch risk** — Go's default path escaping differs from
   AWS's `uriEncode` for chars like `+`/`:`/space, which would silently break signatures.
   Fixed by setting `url.URL.RawPath` to the AWS-style encoding so `RequestURI()` matches the
   signer's canonical URI byte-for-byte.
3. **AC-2's spec pipes `/dev/stdin`** — a pipe stats to size 0, which breaks the
   Content-Length a signed PUT requires. Ported AC-2 to a real temp file; the tool targets
   regular evidence files on disk, not streams.
4. **`go mod tidy` upgraded unrelated deps** — running it in auto-cli bumped parquet-go,
   x/sys, etc. Reverted and added only the `auto-artifact` require+replace by hand; the
   workspace build resolves locally via the replace directive, no tidy needed.
5. **Stale golangci cache from a sibling worktree** polluted `make check` lint with errors
   about files in `auto-stack-wt-041` that don't exist. `golangci-lint cache clean` (not
   `--no-verify`) — matches the documented gotcha.

## Reflections
- **What was tricky?** Getting SigV4 exactly right with zero references. The UNSIGNED-PAYLOAD
  decision (from a resolved design thread) was load-bearing: it let PUT stream the file with
  no buffering/second read AND simplified the canonical request. The payload-hash being a
  separate input from the `x-amz-content-sha256` header is what let the generic AWS vector and
  the S3-specific calls share one signer.
- **What I'd tell myself at the start?** The plan was unusually complete (16 ACs each with an
  executable conformance command, plus resolved review threads pre-answering the SigV4-payload
  and 0600-perms questions). Trust it and go phase-by-phase; the walking-skeleton-first
  ordering meant AC-1 was green against real S3 by end of phase 1, de-risking everything after.
- **What I almost did but didn't?** Dispatch parallel subagents per phase. Backed off: every
  phase touches `conformance_test.go` (and several touch `upload.go`/`client.go`), so parallel
  writers would conflict and serial subagents in a shared worktree risk the documented
  write-leak. Serial self-implementation with full context was the right call.

## Useful context
- `context.md` was excellent — it named the exact mirror module (`auto-env`), the wiring
  checklist from commit `cd80ea9`, and the shared `config.HomeDir()`→`$HOME` mechanism that
  makes temp-`$HOME` test isolation work. Saved a lot of discovery.
- The live bucket + creds in `~/.auto/artifact/settings.json` made the gated conformance suite
  the real executable spec — being able to run all 16 ACs against real AWS locally caught
  signing correctness immediately rather than at CI.
- Decisions D-5 (PUT+DELETE doctor probe, since the IAM user lacks ListBucket/GetObject) and
  D-6 (genuinely-valid `create-role`) were the two non-obvious "don't fake it" calls; both were
  already reasoned out in the plan's decision log.
