# autoartifact

Upload evidence files (screenshots, videos, logs) to a public-read S3 bucket and
get back permanent public HTTPS URLs for embedding in PR comments. Ships as
`auto artifact` in the unified binary.

## Build

```bash
cd auto-artifact
go build ./...
```

The merged `auto` binary is built from the repo root with `make build` (the
artifact tool ships as `auto artifact`).

## Test

```bash
cd auto-artifact
go test ./...        # unit + in-process CLI tests (no network, no creds)
```

### End-to-end conformance (real AWS, gated)

The `conformance/` suite drives the real `auto artifact` CLI against the live
bucket configured in `~/.auto/artifact/settings.json`. It is gated behind
`AUTO_ARTIFACT_E2E=1` so ordinary `go test ./...` and PR CI stay green and free:

```bash
AUTO_ARTIFACT_E2E=1 go test ./auto-artifact/conformance/...
```

It needs valid credentials in `~/.auto/artifact/settings.json` (written by
`auto artifact init`). Object-creating tests use the `7d` tier and self-clean via
`delete`; config-mutating tests run under a throwaway `$HOME`.

## Architecture

- `rootcmd/` — public seam mounted by the merged `auto` binary as `auto artifact`
- `internal/app/` — runtime context (stdout, stderr, cwd)
- `internal/cli/` — Cobra commands (upload, delete, setup, init, doctor, quickstart, docs, update)
- `internal/s3/` — hand-rolled SigV4 signer + thin S3 client (PutObject, DeleteObject, Probe); stdlib crypto only, zero new runtime deps
- `internal/artifact/` — object-key construction, retention tiers, content-type detection, local upload log
- `internal/config/` — settings load/validate (~/.auto/artifact/settings.json)
- `internal/setupscript/` — `setup` provisioning-script generator
- `conformance/` — gated end-to-end acceptance harness

## Security model

- Public-read bucket, `ListBucket` denied; objects are unguessable via UUIDv4 key prefixes.
- Permanent public URLs only — no signed/expiring URLs.
- HTTPS-only — the tool never emits or signs against `http://`.
- `settings.json` holds the secret access key and is written mode `0600`.
