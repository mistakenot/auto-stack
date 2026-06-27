# opensource-ideas.yaml — file format

One doc per project (in a monorepo, one per sub-project, co-located at
`<sub-project>/docs/research/opensource-ideas.yaml`). The `profile` describes
that one scope; `sources` and `ideas` belong to it. IDs are an independent
I-series per doc.

```yaml
profile:
  recorded: 2026-06-18
  project: |
    What this project (or sub-project) is and why it exists.
  architecture: |
    How it's structured, the stack, key conventions. Concrete enough
    to judge whether an upstream idea is additive or a rebuild for us.
  stack: [Python, TypeScript]
  goals: |
    What we're actively trying to improve or build right now.
  non_goals: |
    Explicitly out of scope. Ideas that only serve these are filtered out.

sources:
  - repo: mattpocock/skills
    url: https://github.com/mattpocock/skills
    added: 2026-06-18
    why: |
      A peer skills repo — watch how they structure and distribute skills.
    last_investigated_commit: abc123def          # empty = baseline snapshot next
    last_investigated_at: 2026-06-18T10:30:00Z
    investigations:
      - from_commit: null                          # null = baseline snapshot
        to_commit: abc123def
        investigated_at: 2026-06-18T10:30:00Z
        ideas_added: 5

ideas:
  # newest first
  - id: I001                                       # sequential, zero-padded, per-doc
    title: "Short, descriptive name of the idea"
    source: mattpocock/skills
    commits: [abc123def]                           # inspiring commits (empty for baseline)
    files: [skills/productivity/writing-great-skills/SKILL.md]
    permalink: https://github.com/mattpocock/skills/blob/abc123def/skills/productivity/writing-great-skills/SKILL.md
    discovered: 2026-06-18
    status: candidate            # candidate | shortlisted | accepted | implemented | rejected | deferred
    change_type: additive        # additive | structural-rebuild | replacement
    area: "our subsystem this touches"
    effort: M                    # S | M | L
    impact: high                 # low | medium | high
    risk: low
    confidence: medium
    dependencies: none
    score: 8
    summary: |
      What the idea is, as it exists in their repo.
    application: |
      How WE would apply it here — name the file/subsystem it lands in.
      This is the field that makes the idea actionable.
    evidence: |
      The key mechanism or a short code excerpt, anchored to the source,
      so a reader can reconstruct it without re-cloning.
    notes: |
      Optional — rejection reason, follow-ups, links to our tasks/PRs.
```
