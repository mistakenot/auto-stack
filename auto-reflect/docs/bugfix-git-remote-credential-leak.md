---
hash: "68f8b286"
id: "487f6155"
read_when: "investigating git remote URL sanitization or credential handling in feedback events"
summary: "Bug report and fix scope for autoreflect feedback add leaking git remote credentials into feedback JSONL"
title: "Security Bug: Git Remote Credential Leak in Feedback Events"
---

Title: Security bug: `autoreflect feedback add` leaks git remote credentials into feedback logs

Priority: P0 (security)

Summary:
`autoreflect feedback add` writes `git_remote` into feedback events. If the repo remote URL contains embedded credentials (for example `https://x-access-token:<token>@github.com/org/repo.git`), the token is persisted to disk in `.auto/reflect/feedback.jsonl`. Root cause is insufficient sanitization in `auto-reflect/internal/gitutil/git.go`.

Steps to reproduce:
1. Set remote URL with credentials (tokenized HTTPS URL).
2. Run `autoreflect feedback add --kind missing --comment "test leak"`.
3. Inspect `.auto/reflect/feedback.jsonl`.
4. Observe `git_remote` includes credential material.

Expected:
`git_remote` should be normalized identity only (for example `github.com/org/repo`) with no credentials.

Actual:
`git_remote` includes sensitive userinfo/token when present in remote URL.

Fix scope:
1. Sanitize remote URLs before persistence.
2. Strip URL userinfo, query, fragment, and credentials-like prefixes.
3. Normalize HTTPS/HTTP, SSH, and scp-style remotes to `host/path`.
4. Add tests covering tokenized HTTPS remotes and standard SSH/scp forms.
5. Add regression test confirming feedback events never contain `@` userinfo or token patterns in `git_remote`.
6. Scrub existing `.auto/reflect/feedback.jsonl` entries locally and rotate exposed token if not already rotated.

Acceptance criteria:
1. New feedback events never persist credentials in `git_remote`.
2. Test suite includes credential-leak regression coverage.
3. Existing leaked local feedback entries can be scrubbed with documented remediation.
