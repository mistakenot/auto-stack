# auto-stack/impact-file-dependents-logrus

**Asks:** every file that directly or transitively imports the root `logrus` package.

**Fixture:** logrus v1.9.3 at `d40e25cd45ed9c6b2b66e6b97573a0413e4c23bd`, history stripped, no Go toolchain.

**Ground truth:** `tests/expected.json` holds 17 files. They were computed with
`go list` transitive import resolution at the fixture revision, then cross-checked
against `auto graph` reverse reachability. `solution/answer.json` is a copy used
by the oracle. The two files must stay identical, and `evals lint` checks this.

**Reward:** `reward` is F1. `precision` and `recall` are diagnostic dimensions.

**Provenance:** re-implemented from `testharbor/cases/logrus-importers`.
