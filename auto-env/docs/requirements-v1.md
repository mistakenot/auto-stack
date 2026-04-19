---
hash: "dfccbb6c"
id: "autoenv-v1-requirements"
read_when: "when implementing autoenv v1 or understanding the simplified environment management design"
summary: "V1 spec for autoenv: template-based config file generation with deterministic per-worktree port allocation, supporting init/up/down/status commands with auto-restart and file-only manifest tracking."
title: "AutoEnv V1 Requirements"
---

# AutoEnv V1 Requirements

## Problem

When running multiple git worktrees for the same project (e.g. concurrent coding agents), each worktree needs its own set of dev services on unique ports. Today this requires manually duplicating and editing config files (ecosystem.config.js, Caddyfile, etc.) per worktree. This is error-prone and doesn't scale.

Adjacent pain points that v1 explicitly defers: shared database schema collisions, log interleaving across worktrees, and orphaned processes from abandoned worktrees.

## V1 Scope

A minimal tool that:

1. Templates config files with per-worktree port assignments
2. Places those files into the repo
3. Delegates service lifecycle to user-chosen commands (pm2, docker-compose, npm, etc.)

Explicitly **not** in v1: SQLite state, process-compose dependency, health checks, service log management, MCP server, hooks integration, orphan cleanup (if a worktree is deleted without `autoenv down`, the user is responsible for stopping processes and cleaning up generated files).

## Core Concepts

### Template Files

The directory `.auto/env/files/` contains a file/folder tree that mirrors the repo root. Files in this tree are Go templates. When `autoenv up` runs, the tree is walked, templates are rendered, and output files are placed at the corresponding paths relative to the repo root.

```
.auto/env/files/ecosystem.config.js   →  ./ecosystem.config.js
.auto/env/files/Caddyfile              →  ./Caddyfile
.auto/env/files/web/vite.config.ts     →  ./web/vite.config.ts
```

Any file type is supported (JS, JSON, YAML, TOML, Caddyfile, etc.) as long as it uses the configured template delimiters for variable substitution.

#### Template Delimiters

Default delimiters are Go's standard `{{` and `}}`. Since these collide with JS template literals, Vue/Handlebars, Helm charts, and GitHub Actions expressions, the delimiters are configurable in `config.json`:

```json
{
  "delimiters": ["[[", "]]"]
}
```

When custom delimiters are set, all template files use them:

```js
// With default delimiters:
const port = {{.Port.web}};

// With [[ ]] delimiters:
const port = [[.Port.web]];
const jsTemplate = `Hello {{name}}`;  // JS braces pass through unmodified
```

If custom delimiters are not set, Go's built-in escape works for literal braces: `{{"{{"}}`.

### Template Variables

Templates have access to the following variables:

| Variable | Type | Description |
|---|---|---|
| `.Port.xxx` | int | Auto-allocated port for the named service. One port per unique name. |
| `.Name` | string | Worktree name (directory basename of the worktree). |
| `.Branch` | string | Current git branch name. |
| `.BranchSlug` | string | Branch name sanitized for use in identifiers (see rules below). |
| `.Slot` | int | The resolved slot number (useful for database names, container names). |
| `.RepoRoot` | string | Absolute path to the repo root. |
| `.WorktreePath` | string | Absolute path to the current worktree (differs from RepoRoot in linked worktrees). |

**BranchSlug sanitization rules:**
- Lowercase the entire string
- Replace any character outside `[a-z0-9-]` with a hyphen
- Collapse consecutive hyphens into one
- Trim leading/trailing hyphens
- Truncate to 63 characters (DNS label limit, safe for container names and hostnames)

### Port Allocation

Ports are allocated deterministically with no state file required.

- **Base port**: `3000` (default, configurable)
- **Stride**: `100` (default, configurable)
- **Slot assignment**:
  - The main worktree (not a linked worktree) always gets **slot 0**
  - Other worktrees get a slot derived from a deterministic hash of the worktree name, modulo a reasonable range (e.g. slots 1-99)
- **Port assignment within a slot**:
  - All port variable references are discovered across all template files
  - Port names are sorted alphabetically
  - Each name gets `base + (slot * stride) + index`, where index is the alphabetical position

**Example** (slot 0, base 3000, stride 100):

| Port Name (alpha order) | Index | Port |
|---|---|---|
| `caddy` | 0 | 3000 |
| `db` | 1 | 3001 |
| `firestore` | 2 | 3002 |
| `web` | 3 | 3003 |

Slot 1 would get 3100, 3101, 3102, 3103.

**Known limitations:**

- **Hash collisions**: CRC32 mod 99 has birthday-paradox collisions around ~12 worktrees. Two worktrees hashing to the same slot will bind the same ports — the second `up_command` will fail with a port-in-use error. This is acceptable for v1's single-user scale. Future versions may add a slot registry.
- **Port reshuffling**: Adding a new port name changes alphabetical indexes, shifting existing port assignments. This is by design — run `autoenv down` then `autoenv up` when changing template port names. Adding ports to a running environment without a restart cycle is not supported.
- **No port probing**: autoenv does not check whether allocated ports are already bound. Port conflicts surface as errors from `up_command`.

### Config

Project config lives at `.auto/env/config.json`. Two fields are **required**:

```json
{
  "up_command": "npm run dev",
  "down_command": "pm2 delete all"
}
```

Optional overrides:

```json
{
  "up_command": "pm2 start ecosystem.config.js",
  "down_command": "pm2 delete all",
  "port_base": 4000,
  "port_stride": 50,
  "delimiters": ["[[", "]]"]
}
```

| Field | Required | Default | Description |
|---|---|---|---|
| `up_command` | yes | — | Shell command run after template files are placed |
| `down_command` | yes | — | Shell command run before generated files are removed |
| `port_base` | no | `3000` | Starting port for slot 0 |
| `port_stride` | no | `100` | Port gap between slots |
| `delimiters` | no | `["{{", "}}"]` | Template delimiters (two-element array) |

Commands are single shell strings executed via `sh -c` from the **repo root** directory. For multi-step flows, chain with `&&` (e.g. `"docker compose up -d && npm run dev"`). Array form and multi-command config are deferred to a future version.

## Commands

### `autoenv init`

Scaffolds the `.auto/env/` directory structure:

1. Create `.auto/env/config.json` with empty `up_command` and `down_command` values (`""`)
2. Create `.auto/env/files/` directory
3. Print next-steps instructions to stderr

The empty command values are intentionally invalid — `autoenv up` will fail with a clear error listing exactly which fields need to be filled in. This forces the user to configure real commands before first use.

Idempotent — does not overwrite existing files.

### `autoenv up [--force] [--dry-run]`

1. Read `.auto/env/config.json` — error if missing or lacking required fields
2. **If manifest already exists**: automatically run the `down` sequence first (down_command → delete files → remove manifest), then continue with `up`. This makes `up` idempotent — re-running it restarts the environment without requiring an explicit `down`.
3. Walk `.auto/env/files/` to discover all template files
4. Scan templates for port variable references, collect unique port names
5. Determine worktree name and slot (main = 0, others = hash-based)
6. Compute port map: sort names alphabetically, assign `base + slot*stride + index`
7. Render all templates with the full variable set (ports, name, branch, branch slug, slot, paths)
8. **If `--dry-run`**: print rendered output to stdout for each file (with paths as headers), then exit without writing
9. Check all destination paths — **error** if any file already exists (unless `--force`), listing all conflicts. This catches files that exist but were *not* placed by a previous `autoenv up` (since those were already cleaned up in step 2).
10. Write rendered files to repo root, preserving directory structure
    - `--force` overwrites existing files
11. Write manifest file (`.auto/env/.generated`) listing all placed files
12. Check if any generated paths are not gitignored — warn to stderr if so
13. Run `up_command` via `sh -c` from repo root
14. Print JSON to stdout: `{"name": "...", "slot": 0, "ports": {"web": 3003, ...}}`

Files are written before `up_command` because the command typically needs them (e.g. `pm2 start ecosystem.config.js`). If `up_command` fails, generated files remain on disk for debugging — use `autoenv down` to clean up.

### `autoenv down`

1. Read `.auto/env/config.json` — error if missing
2. Run `down_command` via `sh -c` from repo root
3. If `down_command` fails: report error to stderr, **abort** without deleting files, exit non-zero
4. Read `.auto/env/.generated` manifest
5. Delete all files listed in the manifest (these correspond exactly to paths from `.auto/env/files/`)
6. Remove the manifest file itself
7. Print confirmation to stdout

No directory tracking is needed. The manifest contains only file paths that mirror the `.auto/env/files/` tree. If removing a generated file leaves an empty parent directory, it is left in place — autoenv does not manage directories it didn't create.

`down_command` runs before file deletion because the command may need the generated files (e.g. `docker-compose -f docker-compose.yml down`). If `down_command` fails, files are preserved so the user can fix the issue and retry.

### `autoenv status`

1. Check if `.auto/env/.generated` manifest exists
2. If yes: re-scan templates from `.auto/env/files/` to derive the current port map (same discovery logic as `up`), resolve worktree name/slot, print JSON:
   ```json
   {"provisioned": true, "name": "...", "slot": 0, "ports": {"web": 3003, ...}, "files": ["ecosystem.config.js", ...]}
   ```
3. If no: print `{"provisioned": false}`

Ports are always derived from the current template files, not stored at `up` time. This means `status` reflects any template edits made since the last `up` — useful for seeing what ports *would* be assigned on the next `up`.

The `provisioned` field indicates whether template files have been placed. It does not indicate whether services are actually running — use your process manager's status command for that (e.g. `pm2 status`).

## File Layout

```
<project>/
  .auto/env/
    config.json                    # required, checked into git
    files/                         # template tree, checked into git
      ecosystem.config.js
      Caddyfile
      web/
        some-config.json
    .generated                     # manifest of placed files, gitignored

  ecosystem.config.js              # generated by autoenv up, gitignored
  Caddyfile                        # generated by autoenv up, gitignored
  web/
    some-config.json               # generated by autoenv up, gitignored
```

**Gitignore**: `autoenv up` warns (to stderr) if any generated file path is not covered by `.gitignore`. The user is responsible for adding gitignore entries. The `.auto/env/.generated` manifest should also be gitignored.

## Error Cases

| Condition | Behavior |
|---|---|
| `.auto/env/config.json` missing | Error: `run autoenv init` |
| `up_command` or `down_command` missing from config | Error listing missing fields |
| `.auto/env/files/` empty or missing | Error: no template files found |
| Destination file exists (no `--force`) | Error listing all conflicting files (only for files not placed by a previous `up`) |
| Manifest already exists on `up` | Auto-restart: run `down` sequence first, then proceed with `up` |
| Template syntax error | Error with file path and template error message |
| Port name count exceeds stride | Error: too many ports for configured stride |
| `down` with no `.generated` manifest | Warn, still run `down_command` |
| `up_command` exits non-zero | Error, leave generated files in place for debugging |
| `down_command` exits non-zero | Error, leave generated files in place, abort cleanup |
| Not a git repository | Error: autoenv requires a git repository |
| Symlinks in `.auto/env/files/` | Followed (resolved to target), not copied as symlinks |
| Custom delimiters not a 2-element array | Error: delimiters must be `["open", "close"]` |

---

## Implementation Plan

### Package Structure

```
auto-env/
  main.go
  go.mod
  CLAUDE.md
  cmd/
    root.go
    init.go
    up.go
    down.go
    status.go
  internal/
    config/
      config.go          # load/validate .auto/env/config.json
    port/
      port.go            # slot computation, port map generation
      port_test.go
    template/
      template.go        # discover, scan, render templates
      template_test.go
    worktree/
      worktree.go        # detect worktree name, main vs linked
    manifest/
      manifest.go        # read/write .generated file list
```

### Key Implementation Details

**Config loading** (`internal/config/`):
- `Load(projectRoot string) (*Config, error)` reads `.auto/env/config.json`
- Validates required fields, applies defaults for `port_base` (3000), `port_stride` (100), `delimiters` (`["{{", "}}"]`)
- Struct: `Config{UpCommand, DownCommand string; PortBase, PortStride int; Delimiters [2]string}`

**Worktree detection** (`internal/worktree/`):
- Run `git rev-parse --show-toplevel` and `git worktree list --porcelain`
- Compare current directory against the main worktree path
- If current is main → slot 0. Otherwise hash the worktree basename to a slot (1-99)
- Hash: `crc32(name) % (maxSlots-1) + 1` — deterministic, fast, no state
- Expose: `Info{Name, Branch, BranchSlug string; IsMain bool; Slot int; RepoRoot, WorktreePath string}`

**Template processing** (`internal/template/`):
- `Discover(filesDir string) ([]string, error)` — walk `.auto/env/files/`, return relative paths (follow symlinks)
- `ScanPortNames(filesDir string, paths []string, delimiters [2]string) ([]string, error)` — regex scan for port references using configured delimiters, return sorted unique names
- `Render(filesDir, destRoot string, paths []string, data TemplateData, delimiters [2]string) error` — parse each file as a Go template with custom delimiters, execute with data, write to destRoot preserving structure
- Check for conflicts before writing anything (fail-fast, don't partial-write)

**Port allocation** (`internal/port/`):
- `Allocate(names []string, base, stride, slot int) (map[string]int, error)` — names already sorted, assign `base + slot*stride + i`
- Validate: `len(names) <= stride`, else error

**Manifest** (`internal/manifest/`):
- `.generated` is a newline-delimited list of relative file paths (mirroring `.auto/env/files/` tree)
- No directory tracking — only files are listed
- `Write(path string, files []string) error`
- `Read(path string) (files []string, error)`

**Commands** (`cmd/`):
- Use Cobra. Root command detects project root via `git rev-parse --show-toplevel`
- `init`: scaffold config and files directory
- `up`: orchestrates config → discover → scan → worktree → allocate → render → manifest → gitignore-warn → exec
- `up --dry-run`: render and print without writing or executing
- `down`: config → exec down_command → (abort if failed) → read manifest → delete files → delete manifest
- `status`: check manifest exists, compute port map, print JSON with `provisioned` field

### Build Order

1. `internal/config` — load and validate config (small, no dependencies)
2. `internal/worktree` — detect worktree info (shells out to git)
3. `internal/port` — pure function, easy to unit test
4. `internal/template` — discover, scan, render (depends on port map shape and delimiters)
5. `internal/manifest` — simple file I/O
6. `cmd/init` — scaffold directory structure
7. `cmd/up` — wire everything together
8. `cmd/down` — manifest + exec + cleanup
9. `cmd/status` — read-only, simplest command
10. `main.go` + `go.mod` — entry point

### Testing Strategy

#### Unit Tests

Pure-function and single-package tests. No git repos, no filesystem side effects beyond temp files.

| Package | Test | What it verifies |
|---|---|---|
| `config` | `TestLoadValid` | Parses minimal config (up/down commands only), applies defaults for port_base, port_stride, delimiters |
| `config` | `TestLoadOverrides` | Parses config with all optional fields set, verifies no defaults applied |
| `config` | `TestLoadMissingRequired` | Missing up_command or down_command returns structured error listing missing fields |
| `config` | `TestLoadBadDelimiters` | Delimiters that aren't a 2-element array return error |
| `port` | `TestAllocateBasic` | 4 sorted names, slot 0, base 3000, stride 100 → ports 3000-3003 |
| `port` | `TestAllocateSlotOffset` | Same names, slot 2 → ports 3200-3203 |
| `port` | `TestAllocateExceedsStride` | 101 names with stride 100 → error |
| `port` | `TestAllocateDeterministic` | Same inputs across multiple calls → identical output |
| `worktree` | `TestSlotHash` | Same name → same slot across calls; different names → (likely) different slots |
| `worktree` | `TestBranchSlug` | `feature/FOO--bar` → `feature-foo-bar`; truncation at 63 chars; no leading/trailing hyphens |
| `template` | `TestScanPortNamesDefault` | Discovers `{{.Port.web}}` and `{{.Port.db}}` from file content, returns sorted `["db", "web"]` |
| `template` | `TestScanPortNamesCustom` | With `[[ ]]` delimiters, discovers `[[.Port.web]]`, ignores `{{.Port.fake}}` |
| `template` | `TestRenderBasic` | Renders a template string with port map and name variables, verifies output |
| `template` | `TestRenderCustomDelimiters` | JS file with `{{ }}` literals + `[[ ]]` autoenv vars renders correctly |
| `template` | `TestRenderGoldenFile` | Renders a realistic ecosystem.config.js template, compares against a golden snapshot |
| `manifest` | `TestWriteRead` | Write file list, read back, verify round-trip |
| `manifest` | `TestReadMissing` | Reading a nonexistent manifest returns empty list and no error (for status command) |

#### E2E Tests

Full command-level tests. Each test creates a fresh git repo in `/tmp` with `t.Cleanup` teardown. Commands use echo scripts for `up_command`/`down_command` (e.g. `echo up > marker.txt`) to verify execution without requiring pm2/docker.

| Test | Setup | Steps | Verifies |
|---|---|---|---|
| `TestInitScaffold` | Empty git repo | Run `autoenv init` twice | Creates `config.json` and `files/` dir; second run is idempotent (no overwrite, no error) |
| `TestUpDownHappyPath` | Git repo with config + template files using `{{.Port.web}}`, `{{.Port.db}}`, `{{.Name}}` | `up` → check files → `status` → `down` → `status` | Rendered files exist with correct port values; `status` shows `provisioned: true` with port map; after `down`, files deleted, marker proves `down_command` ran; `status` shows `provisioned: false` |
| `TestUpAutoRestart` | Provisioned environment (manifest exists) | Run `up` again without explicit `down` | Auto-runs `down` first (down_command executes, old files cleaned), then provisions fresh environment |
| `TestUpFileConflict` | Pre-create a file at a destination path | Run `up` without `--force` | Errors listing the conflicting file path; no files written (fail-fast) |
| `TestUpForce` | Same as conflict | Run `up --force` | Overwrites the existing file; renders correctly |
| `TestUpDryRun` | Git repo with config + templates | Run `up --dry-run` | Rendered content printed to stdout; no files written to disk; no manifest created; `up_command` not executed |
| `TestDownCommandFailure` | Provisioned environment; `down_command` set to `exit 1` | Run `down` | Error reported; generated files preserved on disk; manifest still exists |
| `TestWorktreeSlotAssignment` | Git repo with a linked worktree via `git worktree add` | Run `up` from main worktree, then from linked worktree | Main gets slot 0 ports; linked worktree gets offset ports; both render correctly |

No external dependencies beyond git (always available in CI). Worktree detection tested against real `git worktree add` rather than stubs.
