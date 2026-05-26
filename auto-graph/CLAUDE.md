# Autograph

Build and query code context graphs. Supports TypeScript and Go file-level import graphs. TypeScript scanning uses ast-grep; Go scanning uses `go/parser` (no external dependency needed).

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
- Go scanning requires no external dependencies (uses stdlib `go/parser`)
- Run `autograph doctor` to verify all dependencies are satisfied

## Architecture

- `cmd/autograph/` — entry point
- `internal/app/` — runtime context (stdout, stderr, cwd)
- `internal/cli/` — Cobra commands (init, doctor, quickstart, docs, update, code graph, code context)
- `internal/codegraph/` — reusable graph construction (Build, DetectLanguage, DiscoverFiles)
- `internal/config/` — settings loading and validation (~/.auto/graph/settings.json)
- `internal/contextpack/` — context pack model, builder, validation, token estimation, and markdown/JSON renderers
