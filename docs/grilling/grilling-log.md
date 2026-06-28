---
hash: "26aaa85a"
id: "369792d4"
read_when: "reviewing past design-grilling rationale or decisions"
summary: "Log of grill-me design interrogation sessions, capturing questions, answers, and decisions for auto-stack design proposals."
title: "Grilling Log"
---

# Grilling Log

## 2026-06-28 — auto-hook: higher-level hooks abstraction

**Context:** Charlie proposes adding a higher-level hooks layer on top of per-agent hook infrastructure (Claude, Codex, etc.). Instead of raw events (PostToolUse, Stop), define semantic triggers ("agent pushed to PR") that emit cross-agent hints. First use case: remind agents to check for PR review feedback after pushing. Intent: define-once, works-everywhere nudges without per-agent wiring.

**Q — Detection mechanism:** Your load-bearing assumption is that you can detect "agent pushed to PR" from raw PostToolUse events. But agents use different tools. How exactly do you detect "pushed to PR" from the raw hook payload, and what happens when an agent pushes via a method you didn't anticipate?
**A:** Pattern-match tool args — cheapest to build. Parse the normalized hook payload for known git/gh commands. Accept that novel push methods are missed until patterns are added.

**Q — Overlap with autowatch:** Autowatch already has Triggers that fire TaskDefs. Your auto-hook proposal sounds like a second trigger-action system. Why not implement this as an autowatch trigger type?
**A:** Thin layer in `auto hooks fire` — no new system. Just add pattern matching + hint emission directly in the existing command. Simplest, but mixes concerns.

**Q — Hint delivery mechanism:** How does the hint get back to the agent? Claude hooks can return stdout that gets injected. Does Codex support the same? What's the lowest common denominator?
**A:** Different channel per agent — accept that delivery varies (stdout for Claude, maybe different for Codex). The abstraction hides this.

**Context:** Round 2 — pressing on the implications of "thin layer in auto hooks fire." Research confirmed both Claude and Codex support `additionalContext` in hook JSON responses. OpenCode uses TypeScript plugins with no equivalent. Current `auto hooks fire` prints nothing to stdout — adding hint emission changes the command from logger to matcher-responder.

**Q — Latency budget:** `auto hooks fire` runs in the agent's critical path. Adding pattern matching + config loading means every PostToolUse runs through your matcher. What's the latency budget?
**A:** Accept ~10ms overhead. Load a small config file + run patterns per hook call.

**Q — Config location:** Where does the trigger→hint config live? A user wants to define "when any agent pushes to a PR, emit this hint text."
**A:** Project-scoped at `.auto/hooks/hooks.yaml`, version-controlled, discovered from cwd.

**Q — False positives:** What if the matched `git push` command failed? You'd emit a hint for a push that didn't land.
**A:** Check exit code in payload — only emit the hint if the command succeeded.

**Q — Delivery revisit:** Both Claude AND Codex support the same `additionalContext` JSON field. OpenCode is the real outlier (TypeScript plugins). Is "different channel per agent" still the answer?
**A:** Same JSON for Claude+Codex — both support `additionalContext` in `hookSpecificOutput`. Defer OpenCode.

**Context:** Round 3 — pressing on error detection reality (PostToolUse has `is_error` not exit codes), config schema shape, and scope boundary. Research confirmed Claude/Codex PostToolUse provides `is_error` boolean, not numeric exit code.

**Q — Error signal reality:** PostToolUse has `is_error` boolean, not a numeric exit code. Is that enough signal to avoid false-positive hints on failed commands?
**A:** `is_error` boolean is enough — covers Bash (the main case). Write/Edit failures are rare and the hint is low-cost if wrong.

**Q — Config schema:** What does a rule in `.auto/hooks/hooks.yaml` look like?
**A:** Semantic trigger names — named triggers like `pushed-to-pr` map to built-in detection logic in Go code. Config just says `trigger: pushed-to-pr` → hint text. Patterns are in Go, not YAML.

**Q — Scope boundary:** Auto-hook only emits hints, or also blocking/modification?
**A:** Hints only. Auto-hook only emits `additionalContext`. Blocking and modification stay in per-agent hook config. Clean scope boundary.

**Context:** Round 4 — testing whether the "system" is justified given scope decisions. Pressing on extensibility trade-off of Go-coded triggers, premortem failure modes, and the actual trigger inventory.

**Q — Extensibility trade-off:** Semantic triggers in Go means new triggers require code changes. Is that acceptable?
**A:** Built-in triggers are enough. The useful set is small (5-10). Custom patterns are YAGNI.

**Q — Premortem:** 6 months from now, nobody uses auto-hook hints. What went wrong?
**A:** "I don't know."

**Q — Trigger inventory:** What triggers beyond pushed-to-pr justify building this as a system?
**A:** Just pushed-to-pr for now. Ship one trigger, add more if it works.

**Context:** Round 5 — surfacing tension between system-level design choices (YAML config, trigger registry, semantic names) and the actual scope (one trigger, one hint). Also pressing on the premortem timing failure mode.

**Q — Overdesign tension:** Answers contradict: semantic triggers + YAML config + built-in-only + just one trigger. Why not hardcode 10 lines in `auto hooks fire`?
**A:** Config is still worth it — lets users customize the hint text per project without rebuilding. Even with one trigger, the message should be editable.

**Q — Timing failure mode:** PostToolUse hint fires mid-session. Agent has more tool calls queued. Hint gets buried under subsequent results. By stop time, context has scrolled away. Does this concern you?
**A:** "This is why it might fail." Acknowledges this is the likely failure mode. No solution yet.

**Context:** Round 6 — final round. Pressing on the timing problem's solution (Stop-time delivery needs session state) and reversibility of the whole feature.

**Q — Session state for Stop-time delivery:** Stop hook fires once — how does it know the agent pushed during this session? PostToolUse would need to write a marker file that Stop reads.
**A:** Emit on PostToolUse anyway. Accept the timing risk, keep it stateless. Adding state management isn't worth the complexity.

**Q — Reversibility:** What does it cost to rip this out if it doesn't work?
**A:** Easy to rip out. ~50 lines in auto hooks fire + one YAML file. No external dependencies, no schema commitments. Day of cleanup max.

---

### Closing Summary

**Decisions reached:**
1. **Detection:** Pattern-match tool args in PostToolUse payloads (substring/regex on known git/gh commands)
2. **Architecture:** Thin layer in existing `auto hooks fire` — no new system or binary
3. **Delivery:** JSON `additionalContext` in `hookSpecificOutput` — same format for Claude+Codex; OpenCode deferred
4. **Config:** `.auto/hooks/hooks.yaml` project-scoped, version-controlled — maps semantic trigger names to hint text
5. **Triggers:** Semantic names with detection logic in Go code; just `pushed-to-pr` for v1
6. **Error filtering:** Check `is_error` boolean in PostToolUse payload to skip failed commands
7. **Scope:** Hints only — no blocking, no tool input modification, no stop signals
8. **Timing:** Emit on PostToolUse (stateless), accept that hints may get buried mid-session
9. **Latency:** Accept ~10ms overhead per hook call for config loading + pattern matching

**Risks knowingly accepted:**
- **Timing (high):** Hints injected mid-session may scroll out of agent attention window. This was identified as the most likely failure mode. Accepted: keep it simple, see if agents actually act on it before adding complexity.
- **False negatives (low):** Novel push methods (new tools, API calls) won't match until patterns are added.
- **One-trigger YAGNI (medium):** The config file + semantic trigger system may be over-engineered for a single trigger. User justifies it: wants per-project customizable hint text without rebuilding.

**Open questions:**
- Will agents reliably act on `additionalContext` injected during PostToolUse, or does it need Stop-time delivery?
- What second trigger would justify the system beyond pushed-to-pr?
- Should the hint text include dynamic context (PR number, branch name) or stay static?

**No ADR written** — feature is easy to reverse (~50 lines + config file), not deeply surprising, and alternatives weren't hard trade-offs.
