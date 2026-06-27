---
name: release
description: >-
    Create a new auto-stack release by tagging a commit and pushing the tag. The GitHub Actions release workflow builds binaries and publishes the release.
license: MIT
---

# Release

Create a new auto-stack release. The agent's only job is to tag and push — GitHub Actions handles building and publishing.

## How It Works

1. You push a semver tag (e.g. `v0.8.0`) to the repo.
2. The `.github/workflows/release.yml` workflow triggers on `v*` tags.
3. It cross-compiles the single `auto` binary for `linux-amd64` and `darwin-arm64`.
4. It creates (or updates) a GitHub Release with the binaries attached.
5. Users install via `curl -fsSL https://raw.githubusercontent.com/mistakenot/auto-stack/main/install.sh | bash`.

## Steps

### Step 1: Determine the next version

```bash
# Get the latest release tag
LATEST=$(git tag --sort=-v:refname | head -1)
echo "Latest tag: $LATEST"
```

Check what has changed since the last release:

```bash
git log ${LATEST}..HEAD --oneline
```

Apply semver:
- **Patch** (v0.7.0 -> v0.7.1): bug fixes only
- **Minor** (v0.7.0 -> v0.8.0): new features, non-breaking changes
- **Major** (v0.7.0 -> v1.0.0): breaking changes

Ask the user to confirm the version before proceeding.

### Step 2: Verify the build

Run the full build and tests to make sure everything compiles:

```bash
make build
make test
```

Do not proceed if either fails.

### Step 3: Tag and push

```bash
VERSION="v0.8.0"  # use the confirmed version
git tag "$VERSION"
git push origin "$VERSION"
```

### Step 4: Monitor the release

Check that the workflow succeeded:

```bash
# Wait a moment for the workflow to start, then check
gh run list --workflow=release.yml --limit=1
```

If the run fails, check logs with:

```bash
gh run view <run-id> --log-failed
```

### Step 5: Verify the release assets

```bash
gh release view "$VERSION" --json assets --jq '.assets[].name'
```

Expected assets (the single `auto` binary, one per platform):
- `auto-linux-amd64`
- `auto-darwin-arm64`

## Important

- **Do not create releases through the GitHub UI.** Always push tags from the CLI. The workflow creates the release.
- **Do not force-push or move tags.** If a release fails, diagnose and fix the issue, then create a new patch version.
- **All binaries must compile** before tagging. The CI build will fail if any sub-project has errors.
