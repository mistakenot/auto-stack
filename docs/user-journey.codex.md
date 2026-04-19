---
hash: "747d95f2"
id: "8bd7e96d"
read_when: "when confirming auto-stack direction or resolving planning questions"
summary: "Consistency review of the auto-stack user journey, confirming directional decisions and capturing open planning-stage questions for future resolution."
title: "User Journey Consistency Review"
---

# User Journey Consistency Review for `docs/user-journey.md`

This note is only for checking whether the user journey is coherent, directional, and useful.

It is not a feature-by-feature spec. Detailed implementation questions are intentionally deferred.

Use the `Q###` IDs below when referring to open questions.

## Confirmed Direction

- `docs/user-journey.md` is an aspirational guide for the roadmap.
- `docs/user-journey.md` takes priority when shaping future work.
- Examples in the journey should be interpreted pragmatically rather than treated as a frozen CLI contract.
- The goal is to keep the direction consistent and useful first, then get into the weeds of implementation later.
- `autoweb` is a real planned tool, but later.
- `autoweb`, `auto-graph`, and `auto-eval` should be called out as long-term projects. The rest of the stack should be treated as core.
- Use `~/.auto/settings.json` as the shared global settings file.
- Drop `~/.auto/host.json`; keep the host key inside `~/.auto/settings.json`.
- Keep tool-specific settings under `~/.auto/<tool>/settings.json`.
- Default the host identity from the current hostname when the settings file is first created.
- Users can override the host identity later by editing the settings file.
- `autodoc init` should create `./docs` and `.auto/docs`.
- `.autodoc/` is legacy and should be removed from the intended journey.
- The ETL project ambiguity is gone; the repo now has a single ETL project under `auto-etl`.
- The point of the toolchain is to build automated self-improvement feedback loops into codebases, so agents can reflect on their own history, suggest changes, and help the codebase improve over time.
- `autoreflect` and `autowatch` are part of the core journey, not long-term side projects. They should stay in the main narrative, even if the flow stages them after ETL/search where that dependency order makes sense.
- Assume single-host operation first, but ensure all tools support a `host` setting from the start so multi-host support is easier to add later.
- `auto-etl` and `autosearch` are primarily reflection/background tools. In-session coding agents are more likely to consume higher-level tools such as `autoreflect` rather than query ETL/search layers directly.
- The first major milestone is to stand up the full v1 end-to-end flow in this same repository so the stack is dogfooding itself.

## Open Planning-Stage Questions

Q013. How should the journey visually distinguish current behavior, near-term planned changes, and later aspirations?
Q014. Should the journey be reorganized into clear phases, for example: manual setup, assisted workflow, reflection loop, then automation?
Q015. What is the smallest end-to-end slice of this journey that still proves the stack is useful?
Q017. What is the canonical first path through the stack for a new repository, without side branches or optional extras?
Q026. Are ETL and search meant to be part of the active coding loop, or mostly a background/periodic system that feeds later reflection and automation?
Answer: They are primarily background / reflection tools. In-session coding agents will more likely use higher-level tools like `autoreflect` to inform decisions.
Q027. What is the first automation milestone that actually proves value: scheduled ETL, reflection suggestions, automated cleanup branches, or automated PRs?
Answer: The first big milestone is setting up the full v1 end-to-end flow in this same repository so we are dogfooding the tooling.
Q030. What should be removed or simplified in `docs/user-journey.md` because it drags the document into low-level design too early?
Q031. Do we want one unified journey doc, or a short roadmap journey doc plus separate per-tool specs for the details?
