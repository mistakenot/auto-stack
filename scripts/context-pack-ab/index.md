---
name: context-pack-ab
kind: program
services: [question-generator, graph-context, explorer-team, evaluator, reporter]
---

requires:
- project_dir: absolute path to a Go or TypeScript project directory to evaluate against

ensures:
- results: a structured comparison of the graph CLI context pack vs agent-assembled context, with scores and analysis
