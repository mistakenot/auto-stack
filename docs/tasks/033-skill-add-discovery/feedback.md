# Feedback: Task 033 — skill-add-discovery

## Problems faced

1. **Deep-link resolution was implemented but never wired into the pipeline** — `source.SplitDeepLink` + the `RefResolver` interface existed and were unit-tested with a fake resolver, but `add.Run` called `ParseSource` with only `Version` and no resolver. So `/tree/<ref>/<subpath>` URLs left `Ref`/`Subpath` empty and scanned HEAD over the whole repo. Context to understand: the split needs a *live ref set* (only available after the cache repo is cloned), but the canonical URL needed to open that cache only comes from `ParseSource` — a genuine ordering dependency, not an oversight. Resolved with a two-phase parse: parse once (no resolver) → open cache → re-parse with a cache-backed `repoRefResolver`.

2. **`--trust-requested` was a no-op for first-time remotes** — the gate was called with `requestedHosts = nil`, so the documented non-TTY/CI opt-in could never approve an unseen host. Context: the trust design only auto-approves hosts the *project* listed in `skills.yaml` `trusted_hosts` — so the fix was to thread `syaml.TrustedHosts` into `gate.Authorize`, not to blanket-approve arbitrary remotes (which would defeat the gate).

3. **Explicit `--path` discovery skipped the dedupe pass** — default/full-depth ran `dedupeResults` (digest collapse + divergence detection) but explicit mode returned raw results, letting overlapping paths silently last-writer-wins on the lock key.

## Reflections

- **What was tricky:** the deep-link ordering. The natural instinct is "pass a resolver to `ParseSource`", but the resolver depends on a cache that depends on the URL the parse produces. Recognizing it as a two-phase concern (cheap re-parse once the repo is open) was the unlock.
- **What I'd tell myself at the start:** when a layer ships a clean, unit-tested primitive (the `RefResolver` split), check that the *caller* actually constructs and passes a real implementation — a green primitive test says nothing about whether it's wired.
- **What I almost did but didn't:** I almost made `--trust-requested` authorize any first-time remote (which is how the reviewer phrased the symptom). That would have widened the trust surface; the design intent is host-list opt-in, so I scoped the fix to `trusted_hosts`.

## Useful context

- `docs/tasks/033-skill-add-discovery/context.md` — the dependency-contract notes for 032 (schemas) and 009 (cache/trust) made it clear the trust gate's `requestedHosts` maps to `skills.yaml trusted_hosts`, which pinned down fix #2.
- `auto-skill/internal/source/{source,deeplink}.go` — `ParseSource` re-canonicalizes the repo prefix even without a resolver, so a second parse pass is safe and deterministic.
- file:// fixtures can't exercise the deep-link path end-to-end (`isLocalPath` short-circuits before deep-link detection), so the adapter is covered by `TestRepoRefResolver` against a real branch in a cache-opened repo, with the split itself covered by the source package's fake-resolver tests.
