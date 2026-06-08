# auto-config

Validate and manage coding agent configuration.

## Build

```bash
cd auto-config && go build ./...
```

The merged `auto` binary is built from the repo root with `make build` (the config tool ships as `auto config`).

## Test

```bash
cd auto-config && go test ./...
```

## Lint

```bash
cd auto-config && go vet ./...
```
