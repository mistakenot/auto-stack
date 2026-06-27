# Context: Task 043 — etl-registry-repo-discovery

Codebase grounding for gating auto-etl's GitHub + git-history repo discovery to projects registered in `~/.auto/projects.json`. See [plan.html](plan.html).

## Key Files

### The discovery boundary (where the gate goes)
- `auto-etl/cmd/run.go:74` — `remotes := loadRemotesCache()` loads the shared `map[workspacePath]remoteURL`.
- `auto-etl/cmd/run.go:78-82` — `runSessionETL(hostID, remotes)` populates `remotes` in place (one entry per session `cwd`). **Unfiltered — stays that way.**
- `auto-etl/cmd/run.go:85-90` — `runGitHubSync(ctx, hostID, remotes, explicitGitHub)`; `explicitGitHub = len(onlyFlag) > 0 && !sources["sessions"]`.
- `auto-etl/cmd/run.go:93-97` — `runGitETL(hostID, remotes, repoPathFlag, sinceFlag, fullRun)`.
- `auto-etl/cmd/run.go:107` — `saveRemotesCache(remotes)` persists the **unfiltered** cache (must not be gated).
- `auto-etl/cmd/run.go:230` — `repos := ghclient.DiscoverRepos(remotes)` — iterates map **values only**, so it has the remote URL but **not** the workspace path.
- `auto-etl/cmd/run.go:395` — `repos := gitextract.DiscoverRepos(remotes, explicitPaths)` — iterates map **keys** (paths) + `--repo-path` explicit paths.

### Discovery functions (stay registry-agnostic)
- `auto-etl/internal/github/repo.go:31-59` — `DiscoverRepos(remotes map[string]string) []RepoRef`; dedupes by `owner/repo`, drops the path. `RepoRef{Owner, Repo, GitRemote}`.
- `auto-etl/internal/git/discover.go:19-53` — `DiscoverRepos(remotes, explicitPaths) []RepoInfo`; resolves each path to git toplevel, dedupes. `RepoInfo{Path, Remote}`.

### The remotes cache
- `auto-etl/cmd/run.go:316-323` — `etlSettings{Remotes map[string]string}` at `~/.auto/etl/settings.json`. Key = workspace cwd path, value = origin remote URL.
- `auto-etl/cmd/run.go:325-365` — `loadRemotesCache` / `saveRemotesCache` / `resolveGitRemote`.

### The registry (what we filter against)
- `auto-shared/config/projects.go:22-36` — `ProjectRef{ID, Path, Remote, Name, Tools, RegisteredAt}`, `ProjectsConfig{Projects []ProjectRef}`.
- `auto-shared/config/projects.go:39-45` — `ProjectsConfigPath()` → `~/.auto/projects.json`.
- `auto-shared/config/projects.go:78-87` — `LoadProjects(path)` (lenient, nil → empty slice).
- `auto-shared/config/projects.go:192-206` — `FindProjectByPath(dir)` — **longest-prefix** match (matches root and nested subdirs); returns nil for unrelated dirs.
- `auto-shared/config/projects.go:225-239` — `FindProjectByRemote(remote)` — normalizes **both** sides via `git.NormalizeRemoteURL` (SSH→HTTPS, strip `.git`, strip credentials, lowercase host); nil if no match / empty.
- `auto-etl/go.mod:7,12` — already depends on `github.com/mistakenot/auto-shared` with `replace => ../auto-shared`. `cmd/run.go:15` already imports `sharedconfig "…/auto-shared/config"`. **No new dependency.**

## Patterns

- **Quiet read-only registry load** (don't mutate registry from ETL): `auto-cli/cmd/auto/hookscmd.go:302-315` `loadRegistryQuietly()` — `ProjectsConfigPath` → `os.Stat` → `LoadProjects`, returns empty `ProjectsConfig{}` on any error. Mirror this; do **not** call `EnsureProjects()` (it creates/migrates the file as a side effect).
- **Path-then-remote resolution** (the canonical dual lookup): `auto-cli/cmd/auto/hookscmd.go:215-229` — try remote first then path. We invert to **path-then-remote** per requirements, but the structure is identical.
- **Registry as authority** (unregistered → nothing): `docs/auto-bus-spec.md:332-335` — "The registry is the authority — an unregistered or unknown project derives nothing." Our strict empty-registry behavior mirrors this exactly.
- **Diagnostics to stderr** (existing convention in these phases): `internal/github/repo.go:55` and `internal/git/discover.go:36` already `fmt.Fprintf(os.Stderr, …)` for warnings. `auto-etl run` prints progress to stdout (`run.go:71-72,112`); our gate summary goes to **stderr** to match the existing warning style.
- **Test fixture for registry**: `auto-shared/config/projects_test.go:121-166` uses `t.TempDir()` + `t.Setenv("HOME", home)` + seed `~/.auto/projects.json`. `FindProjectByPath` longest-prefix test at `:31-46`; `FindProjectByRemote` normalization test at `:204-258`.
- **Existing discovery tests** (stay green, registry-agnostic): `auto-etl/internal/github/repo_test.go:50-92` builds `remotes` maps and asserts dedup/host-filtering.
- **CLI conventions** (root `CLAUDE.md`): JSON stdout / diagnostics stderr; every hard error carries a remediation hint (e.g. "run `auto init --project`").

## Constraints from docs

- `auto-etl/docs/github-pr-etl.md:425-440` currently documents the **opposite** policy: "all GitHub repos in the global cache are synced … This is intentional." This doc must be updated — the gate is a deliberate policy reversal.
- `auto-etl/docs/git-history-etl.md:227-228` documents auto-discovery from session workspaces + `--repo-path`. Must note the registry gate (and that explicit `--repo-path` bypasses it — see decision in plan).
- `auto-etl/CLAUDE.md` decision principle: "Keep as much original data as possible … immutable." Gating is non-destructive (skips, never mutates the cache or parquet), so it complies.

## Related Tasks
- **Task 032** (skill-project-schemas) — introduced the registry: commit `66e9c4c` (PR #72) added `auto-shared/config/projects.go` (`ProjectRef`, `ProjectsConfig`, `LoadProjects`, `FindProjectByPath` longest-prefix, `FindProjectByRemote` normalized). Follow-up `ebfbc3b` (#73) fixed `EnsureProjects` legacy migration.
- **Task 021** (auto-bus-standard, merged, `e3a635b`/#75) — established "registry is the authority / unregistered derives nothing" via `DeriveDocChanged(ev, reg)` using `FindProjectByRemote`. We mirror this principle. See `docs/auto-bus-spec.md:332-335` and `docs/tasks/021-auto-bus-standard/plan.md` §Phase 1.
- **Task 024** (planning-dashboard-backend, merged) — `project.list` RPC loads the registry via a provider func and normalizes remotes at the boundary (`git.NormalizeRemoteURL`). Same load-then-lookup shape we use.
- `auto init --project` (`auto-cli/cmd/auto/initcmd.go`) — the registration flow our stderr remediation hint points users to.

## Git history / drift check (CB3, verified)
- All Solution-tab paths and line numbers confirmed present, **no drift**: `runGitHubSync` (run.go:212-286), `runGitETL` (376-465), DiscoverRepos calls (230, 395), `loadRemotesCache`/`saveRemotesCache` (325-349). `sharedconfig` already imported at `run.go:15` — no new dependency.
- Most recent touch to `cmd/run.go` is `1b1dada` feat(022) which added `runHooksETL` (lines 99-104) — **no conflict**; the gate inserts cleanly after `runSessionETL` (run.go:82) and before `runGitHubSync` (run.go:85).
- The "sync all discovered repos" behavior originated in `50eaefc` (GitHub PR ETL) and `8a22803` feat(002) (git ETL) — this task deliberately reverses that policy.
