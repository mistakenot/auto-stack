#!/usr/bin/env bash
# Generate the two Harbor task arms for a CASE:
#   out/<slug>/with-graph/  — agent has the `auto` binary on PATH (treatment)
#   out/<slug>/baseline/    — same task + verifier, no `auto` (control)
#
# A case (cases/<slug>/) supplies the seed repo (case.env), the question
# (instruction.md), the neutral ground truth (expected.json), and an Oracle
# (solve.sh). Everything else — Dockerfiles, verifier, task.toml — is shared, so
# the only differences between arms are (a) whether the binary is present and
# (b) one sentence in the instruction naming the tool.
#
#   CASE=gogit-deep-trace ./render.sh
#   CASE=logrus-importers ./render.sh
set -euo pipefail

CASE="${CASE:?set CASE=<slug>; available: $(ls "$(dirname "$0")/cases" 2>/dev/null | tr '\n' ' ')}"
ARCH="${ARCH:-amd64}"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(git -C "$HERE" rev-parse --show-toplevel)"
SHARED="$HERE/shared"
CDIR="$HERE/cases/$CASE"
[ -d "$CDIR" ] || { echo "!! no case '$CASE' in cases/"; exit 1; }
# shellcheck disable=SC1091
. "$CDIR/case.env"

echo ">> building static auto (linux/$ARCH)"
make -C "$REPO" dist GOOS=linux GOARCH="$ARCH" >/dev/null
BIN="$REPO/dist/auto-linux-$ARCH"
[ -f "$BIN" ] || { echo "!! expected $BIN"; exit 1; }

OUT="$HERE/out/$TASK_SLUG"

# render <arm> <dockerfile-tmpl> <with-binary:0|1> <tool-hint>
render() {
  local name="$1" tmpl="$2" with_bin="$3" hint="$4"
  local dst="$OUT/$name"
  rm -rf "$dst"
  mkdir -p "$dst/environment" "$dst/tests"

  sed -e "s#__REPO_URL__#${REPO_URL}#" \
      -e "s#__REPO_SHA__#${REPO_SHA}#" \
      -e "s#__REPO_DIR__#${REPO_DIR}#" \
      "$SHARED/$tmpl" > "$dst/environment/Dockerfile"
  [ "$with_bin" = "1" ] && cp "$BIN" "$dst/environment/auto"

  awk -v hint="$hint" '{ gsub(/__TOOL_HINT__/, hint); print }' \
    "$CDIR/instruction.md" > "$dst/instruction.md"
  sed "s#__NAME__#auto-stack/${TASK_SLUG}-${name}#" \
    "$SHARED/task.toml.tmpl" > "$dst/task.toml"

  cp "$SHARED/tests/test.sh" "$SHARED/tests/score.py" "$dst/tests/"
  cp "$CDIR/expected.json" "$dst/tests/expected_importers.json"

  if [ "$with_bin" = "1" ]; then
    mkdir -p "$dst/solution"
    cp "$CDIR/solve.sh" "$dst/solution/solve.sh"
    chmod +x "$dst/solution/solve.sh"
  fi
  chmod +x "$dst/tests/test.sh"
  echo ">> rendered $TASK_SLUG/$name  (binary=$with_bin)"
}

render with-graph Dockerfile.with.tmpl 1 \
  $'\nThe `auto` CLI is on your `PATH`; run `auto graph quickstart` to see how it builds file-level import graphs. Use whatever tools you find most effective.'

render baseline Dockerfile.baseline.tmpl 0 \
  $'\nUse whatever tools you find most effective.'

echo ">> done. run e.g.:  harbor run -p $OUT/with-graph -a oracle"
