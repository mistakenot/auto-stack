# Feedback: Task 037

## Problems faced
1. **CI red while local green** — the synthetic vercel fixture lived under
   `auto-skill/internal/migrate/testdata/`, which `.gitignore`'s `**/testdata/` rule
   silently excludes. `git add <dir>` added nothing (no error), so the file was never
   committed; every `migrate` test that reads it passed locally (file on disk) but failed
   on CI with `open testdata/...: no such file or directory`. Fix: add an explicit
   `!auto-skill/internal/migrate/testdata/` un-ignore line (the repo's established pattern,
   cf. `!auto-graph/testdata/`) and commit the fixture.
2. **Hooks invoked a non-existent flag** — the hook stanzas called
   `auto skill sync --check --format json` / `lint --format json`, but `sync` registers no
   `--format` flag (only `--text`) and `lint` uses `--text/--json`. In a real native-skills
   repo the pre-commit gate would have died on an unknown-flag error (and post-merge sync
   silently no-op'd). Caught by Codex review, not by the wiring tests (the stub `auto`
   accepted any argv). Fix: drop `--format json` everywhere (all three commands default to
   JSON).
3. **Local git source stored a non-clonable path** — `isGitRepo` returns true for a
   *subdirectory* of a worktree (it uses `--is-inside-work-tree`), so a vercel `local`
   source like `/repo/skills` was stored as the lock `URL`, which a later `git clone`
   can't address. Fix: resolve `git rev-parse --show-toplevel` for the `URL` and put the
   relative dir in `Subpath`.

## Reflections
- **What was tricky?** The two highest-impact bugs were both invisible to the local test
  suite — a gitignored fixture and a CLI flag that only fails against the *real* binary
  (the hook tests stub `auto`, so they never validate that the subcommands actually accept
  the flags passed). Both surfaced only at CI / external review.
- **What would you tell yourself at the start?** Before wiring a shell hook to a CLI,
  grep the target command's actual `Flags()` registrations — don't assume a flag exists
  because a sibling command (`migrate`, `update`) has it. And after creating any
  `testdata/` fixture, immediately run `git check-ignore` on it.
- **What did you almost do but didn't?** Almost left `--format json` on the hook calls
  because "JSON is the house default" — but the flag itself doesn't exist on `sync`/`lint`,
  which is the opposite failure mode.

## Useful context
- `auto-shared/config/validation.go` (`ValidationError{Code,Path,Field,Message,Value}`) and
  `auto-skill/internal/skill/{lock,skillsyaml,version}.go` are the load-bearing schema
  contracts — `versionFromRef` output must pass `ValidateVersionSpec`; lock entries must
  pass `ValidateLock` with `state:"unresolved"` (no commit required in that state).
- The hook tests drive the **real** Makefile targets hermetically via
  `make -C <tmpProject> -f <repoRoot>/Makefile <target>` with a stubbed `auto` on PATH —
  `-C` sets `$(CURDIR)` to the temp project so the `.auto/skills/lock.json` guard resolves
  there. This is the pattern to reuse for any future shell-wiring verification, but note it
  validates wiring/exit-semantics only, not that the invoked CLI flags exist.
- `internal/add` holds unexported `isGitRepo` / copy-with-containment helpers; `migrate`
  mirrored their safety rather than reusing them (scope boundary). A future cleanup could
  promote one shared exported helper.
