# auto-stack/impact-pkg-dependents-gogit

**Asks:** the package-level reverse-dependency set of `utils/trace` in go-git.

**Fixture:** go-git at `302dddeda962e4bb3477a8e4080bc6f5a253e2bb`, history stripped, no Go toolchain.

**Ground truth:** `tests/expected.json` holds 27 packages. They were computed with
`go list` transitive import resolution at the fixture revision, then cross-checked
against `auto graph` reverse reachability. `solution/answer.json` is a copy used
by the oracle. The two files must stay identical, and `evals lint` checks this.

**Reward:** `reward` is F1. `precision` and `recall` are diagnostic dimensions.

**Provenance:** re-implemented from `testharbor/cases/gogit-deep-trace`.
