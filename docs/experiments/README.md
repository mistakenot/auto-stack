# Experiments

Long-lived writeups for research-style experiments run against auto-stack. Code, data, embeddings, and plots live in `.tmp/experiments/<name>/`; the markdown findings live here and are checked into git.

## Conventions

- One folder per experiment program, prefixed with the start date: `YYYY-MM-DD-<name>/`
- Each folder contains a `README.md` synthesizing the whole experiment
- Phase or sub-experiment docs are named `phaseN-<topic>.md`
- See [PATTERNS.md](PATTERNS.md) for the dispatch and end-of-experiment checklists and the patterns/anti-patterns observed across runs

## Experiments

- **[2026-05-26 — Orthogonal Questioning](2026-05-26-orthogonal-questioning/README.md)**: Tested whether requirements could be modeled as a vector space and compressed to ~3 questions via cosine-geometry orthogonal probing. Four phases. Conclusion: the geometric framework as originally proposed doesn't work, but a relaxed version using per-dimension classifiers + active learning hits the same 3-5 question budget on linguistically-legible preference dimensions.

- **[Structured Compiler](structured-compiler/)**: (separate experiment, see folder)

## See also

- [PATTERNS.md](PATTERNS.md) — patterns and anti-patterns for running experiments
- `.tmp/experiments/` — code, data, and intermediate artifacts (not in git)
- `docs/spikes/` — earlier-style spike docs (predates this folder convention; new work should go here instead)
