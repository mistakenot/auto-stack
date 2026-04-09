---
hash: ""
id: "8c156daa"
summary: "Requirements for autoskill: agent skill management, discovery, and distribution"
title: "autoskill — Requirements"
---

# autoskill — Requirements

## Drift Detection

- Detect drift between skill content and documentation content
- Skills often encode rules/patterns that originate from docs — when the doc changes, the skill may become stale
- Use autodoc-style hash-based freshness links between skills and their source docs
- Flag skills whose referenced docs have changed since the skill was last updated
- Surface in `autoskill doctor` or `autoskill lint` output
