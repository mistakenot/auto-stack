# Autograph

Build and query code context graphs. Currently supports TypeScript file-level import graphs via ast-grep.

## Build

```bash
cd auto-graph
go build ./cmd/autograph/
```

## Test

```bash
cd auto-graph
go test ./...
```

## Vet

```bash
cd auto-graph
go vet ./...
```

## Dependencies

- `ast-grep` must be installed for TypeScript scanning (`npm install -g @ast-grep/cli`)
- Run `autograph doctor` to verify all dependencies are satisfied

## Architecture

- `cmd/autograph/` — entry point
- `internal/app/` — runtime context (stdout, stderr, cwd)
- `internal/cli/` — Cobra commands (init, doctor, quickstart, docs, update, code graph)
- `internal/config/` — settings loading and validation (~/.auto/graph/settings.json)
