# Feedback: Task 009

## Problems faced
1. Pre-commit lint caught an unchecked json.Marshal error in the new test file -- the errchkjson linter flags `any`-typed marshal calls, easy fix but blocked the first commit attempt.

## Reflections
- The file:// trust bypass was subtle: `parseURLParts` correctly separated host and path for file:// URLs, but the `Endpoint()` function only used `host` (empty for file://) and ignored the path. The fix was straightforward once the data flow was traced.
- The symlink-escape fix required replacing `os.MkdirAll` with a component-by-component `Lstat` walk. The two-pass extract design (validate then write) already caught archive-embedded symlinks, but couldn't detect pre-existing filesystem symlinks in the destination directory.
- The multi-project prune fix was the most architecturally interesting: the global cache is shared across projects, so unreferenced pruning must check ALL projects in `~/.auto/projects.json`, not just the current working directory's lock file.

## Useful context
- `auto-shared/config/projects.go` has `ProjectsConfigPath()` and `LoadProjects()` for reading the global registry -- reused directly in the fix.
- The existing `CanonicalizeURL` function already handled file:// correctly; only `Endpoint()` (trust identity) had the collapse bug.
