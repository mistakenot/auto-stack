# Find every file that imports a package

A Go repository is checked out at `/app/logrus`.

Find **every file** in that repository that imports the root `logrus` package —
the package containing `logrus.go` — either **directly** or **transitively** (a
file that imports another package which, in turn, imports `logrus`, and so on).

Write your answer to `/app/answer.json` as a JSON array of repository-relative
file paths, for example:

```json
["hooks/test/test.go", "example_hook_test.go"]
```

Include a file if any of its imports lead to the `logrus` package. Do **not**
include the files that make up the `logrus` package itself (they do not import
it).
__TOOL_HINT__
