# Context: Task 051 — auto-artifact-cli

Codebase grounding for the `auto artifact` S3 upload CLI. See [plan.html](./plan.html).

## Key Files

### Module skeleton to mirror (smallest existing module: `auto-env`)
- `auto-env/go.mod` — module shape: `module github.com/mistakenot/auto-env`, `go 1.26.1`, requires `auto-shared v0.0.0` + `cobra v1.10.2`, `replace github.com/mistakenot/auto-shared => ../auto-shared`.
- `auto-env/rootcmd/rootcmd.go` — the public seam mounted by auto-cli:
  ```go
  func New(stdout, stderr io.Writer) *cobra.Command {
      cwd, _ := os.Getwd()
      return cli.NewRootCmd(app.New(stdout, stderr, cwd))
  }
  ```
- `auto-env/internal/app/app.go` — `App` struct carrying `Stdout, Stderr, CWD`.
- `auto-env/internal/cli/root.go` — root cobra command, `SilenceErrors: true`, `SilenceUsage: true`, `ExitError{Code, Err}` type.

### Wiring points (the merged binary)
- `auto-cli/cmd/auto/main.go:13-24` — import block of `*cmd "github.com/mistakenot/auto-*/rootcmd"`.
- `auto-cli/cmd/auto/main.go:39-50` — `root.AddCommand(...)` block where `artifactcmd.New(stdout, stderr)` is added.
- `auto-cli/go.mod` — needs `require github.com/mistakenot/auto-artifact v0.0.0` + `replace ... => ../auto-artifact`.
- `go.work:3-16` — `use (...)` block; add `./auto-artifact`.
- `Makefile` `PROJECTS :=` list (~line 16) — add `auto-artifact`.

### Shared helpers to reuse (`auto-shared/config/`)
- `HomeDir() (string, error)` — **prefers `$HOME` env var**, falls back to `os.UserHomeDir()`. This is what makes the temp-`$HOME` test isolation (D-4) work — tests do `t.Setenv("HOME", dir)`.
- `AutoDir() (string, error)` → `~/.auto`; `EnsureAutoDir()`.
- `DecodeJSONFileStrict(path, &target)` — reject unknown fields; `DecodeJSONFile` for forward-compat.
- `WriteJSONFile(path, value)` / `WriteJSONFileAtomic(path, value)` — indented JSON + trailing newline, creates parent dirs.
- `ValidationError{Code, Path, Field, Message, Value}` + `ValidationErrorsError{Path, Errors}` in `auto-shared/config/validation.go` — the shared validate() shape mandated by CLAUDE.md.
- `version.Version` (`auto-shared/version/version.go`) — `dev`, overridden via ldflags.

### Config pattern to copy (`auto-search/internal/config/settings.go`)
- `XxxSettingsPath()` → `filepath.Join(toolDir, "settings.json")`; `LoadXxxSettings(path)` (strict decode + validate); `EnsureXxxSettings()` → `(path, cfg, created, error)`; `ValidateXxxSettings(path, cfg) []ValidationError`.

### CLI conventions in practice
- JSON default + text flag: `auto-search/internal/cli/search.go:98-131` — `json.NewEncoder(cmd.OutOrStdout())` with `SetIndent("", "  ")`; `--text` short-circuits to a text renderer. (Requirements here specify the flag spelling `--format text`.)
- `doctor` JSON checks: `auto-graph/internal/cli/doctor.go:19-41` — array of `{check, status(pass|warn|fail), message, hint}`, exit 1 if any fail.
- `quickstart`: `auto-search/internal/cli/quickstart.go:477-487` — prints an embedded markdown const.
- stderr vs stdout + remediation hints: errors returned as `ExitError{Code:1, Err}`; CLAUDE.md requires every hard error carry a hint (e.g. "run `auto artifact init`").

### HTTP / crypto baseline
- Only `auto-etl/internal/github/fetch.go` uses `net/http` (GitHub API). **No AWS SDK, no SigV4, no `crypto/hmac` anywhere in the repo** — we add the SigV4 signer from scratch (Q-1: zero deps, stdlib `crypto/hmac`, `crypto/sha256`, `net/http`).

### Existing artifact assets (already in repo)
- `auto-artifact/docs/requirements.md` — authoritative spec + the 16 ACs (each with an executable bash conformance command).
- `auto-artifact/scripts/setup-aws.sh` — provisions bucket `auto-artifact-datadyne` / `eu-west-1`: public-read bucket policy (`s3:GetObject` allow `*`, `s3:ListBucket` deny `*`), 4 prefix lifecycle rules (7d/30d/90d/365d), IAM user `auto-artifact-uploader` + policy (`s3:PutObject`,`s3:DeleteObject`), access keys. `auto artifact setup` must emit an equivalent parameterized script (AC-10 expects `create-bucket`, `put-bucket-policy`, `put-bucket-lifecycle-configuration`, `create-role`, `create-user`, `create-access-key`).

## Patterns
- **Tests**: `t.TempDir()` + `t.Setenv("HOME", home)` to isolate config; a `runCLI(t, args...) (stdout, stderr, code)` helper that builds `app.New(...)` + `cli.NewRootCmd(...)` and runs in-process. Integration tests live in `internal/cli/*_test.go`, package `cli_test`. Env-gated/integration tests are rare in-repo — we introduce `AUTO_ARTIFACT_E2E=1` gating (D-3) for the real-AWS conformance suite.
- **Package layout** (`docs/auto-package-patterns.md`): all impl under `internal/`; one file per command in `internal/cli/`; domain logic separate from CLI wiring; baseline commands `init`/`doctor`/`quickstart`/`docs`/`update`.
- **CI** (`.github/workflows/ci.yml`): every module auto-included in `make check/build/test/vulncheck` once in `PROJECTS`. No per-module job needed. The E2E suite needs a separate `workflow_dispatch` job with AWS secrets (Q-2).
- **CLAUDE.md** per module is required (tool name, build/test commands, autodoc index placeholder).

## Related Tasks
- Task 050 (model-based-sync-testing) — most recent planning-doc; mirrored its `pd-meta` block + tab structure.
- `new-package` skill (`.claude/skills/new-package/SKILL.md`) — produces the exact skeleton (rootcmd + internal/{app,cli,config} + go.mod + CLAUDE.md); we follow it but add the SigV4 client + S3 domain package + the conformance harness it deliberately omits (skill does not scaffold tests).

### Git history (CB3)
- **`cd80ea9`** (feat 017: unify binaries into a single `auto`) — the canonical precedent for adding a module to the merged binary. Complete wiring checklist: `go.work` use-block · `Makefile` `PROJECTS` · `<mod>/rootcmd/rootcmd.go` seam · `auto-cli/cmd/auto/main.go` AddCommand · `auto-cli/go.mod` require+replace · **`auto-cli/cmd/auto/main_test.go`** (has a stems list asserting each subcommand mounts — add `"artifact"`) · root `CLAUDE.md` sub-projects table.
- **`539f702`** — added `auto-artifact/docs/requirements.md` + `scripts/setup-aws.sh`. **`08d7f93`** — the CloudShell fix (this session).
- **No prior S3 / SigV4 / AWS / upload code anywhere in the repo** — confirmed. We define the SigV4 signer and the env-gated live-integration test pattern from scratch (no in-repo precedent for either).
- E2E precedents (fixture-based, *not* env-gated, no external service): `auto-etl/e2e_test.go`, `auto-env/e2e/e2e_test.go`; in-process CLI helper `auto-search/internal/cli/cli_integration_test.go` (`runCLI`).

### Drift checks (verified current)
- `Makefile` `PROJECTS :=` line does **not** contain `auto-artifact` (needs adding).
- Root `CLAUDE.md` sub-projects **table** has no `auto-artifact` row (the autodoc *index* mentions it, but the table doesn't) — add a row.
- `.github/workflows/` has `ci.yml` (no `workflow_dispatch`, no AWS secrets), `release.yml`, `claude-review.yml` (disabled). The manual E2E job (Q-2) is a **new** `workflow_dispatch` workflow — no existing one to extend.
- Mount seam spelling confirmed: `rootcmd.New(stdout, stderr io.Writer) *cobra.Command` (per `auto-cli/cmd/auto/main.go:39-50`), which internally calls `cli.NewRootCmd(app.New(...))`.

## Constraints / risks
- **No-clobber**: AC-11/AC-12b/AC-14 mutate or remove `settings.json`; they must run under a temp `$HOME` (D-4), relying on `auto-shared` `HomeDir()` honoring `$HOME`.
- **Real-bucket side effects**: the live conformance suite writes real objects to `auto-artifact-datadyne`; use the `7d` tier and `delete` to self-clean. Credentials in `~/.auto/artifact/settings.json` (mode 600) will be rotated later.
- **SigV4 correctness** is the main implementation risk — it's hand-rolled and there's no in-repo reference. Mitigate with unit tests against AWS's published SigV4 test vectors before the live suite.
