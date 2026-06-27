# Feedback: Task 044

## Problems faced
1. **Sub-agent correlation has no id join** — the parent's `Agent` tool-call result does not record the spawned session id, so a dispatch can't be linked to a child by id. Resolved by grouping children on `parent_session_id` and matching the dispatch `prompt` against each child's **full** `FirstUserIntent` (via `GetSessionByID`, not the 200-char `SessionListRow` preview) in dispatch order. Verified against a real `/execute-task 041` tree.
2. **`SessionMessages` omits `tool_use_result_json`** — only `GetMessageByID` SELECTs it. The builder falls back to the `tool_result` row's `content`, which is populated for the common cases (Bash/file/search). Structured-only envelopes (e.g. AskUserQuestion) show empty drill-down output — an accepted, deferred gap (do NOT silently assume the field is available off `SessionMessages`).
3. **Shared test fixture is load-bearing** — `testutil/fixtures.go` feeds ~14 message-count assertions across 5 test files. Adding the required paired `Agent` dispatch + paired `Bash` rows rippled counts (13→17, session-1 9→13, non-thinking 8→12); all dependent assertions had to move together.
4. **The existing fixture Bash row never rendered** — `msg-005` is a standalone `role=tool` Bash row with no `tool_use_id`; the builder only emits Bash events from assistant `tool_use` rows paired by `tool_use_id`. AC-3 depended on adding genuinely paired rows.

## Reflections
- **What was tricky:** the content-fidelity default flip-flopped during planning (truncated → full). The review (Grok) correctly caught that the decision hadn't propagated to every section. Lesson: when a default reverses, grep the whole doc for the old framing — Requirements, Out-of-scope, Known-gaps, and the Plan steps all carried stale copies.
- **What I'd tell myself at the start:** build the model + renderer as **pure** functions first (no CLImixed in) — it made the prototype port and the script-safe-escaping tests trivial, and kept the CLI phase thin.
- **What I almost did but didn't:** extend `SessionMessages` to load `tool_use_result_json` mid-task — pulled back to an accepted/deferred gap per direction, avoiding scope creep into the shared query.

## Useful context
- The Python prototype (`artifacts/prototype-build_doc.py` + `prototype-template.html`) was a faithful spec — the Go builder is a near-direct port. Keep prototypes around; they de-risk the real implementation.
- **Script-safe escaping is mandatory** when injecting the model JSON into a `<script>`: marshal with `SetEscapeHTML(false)` then explicitly replace `</` → `<\/` and U+2028/U+2029, or message content containing `</script>` breaks the page. Make it an explicit, testable step rather than relying on Go's silent HTML escaping.
- `GetSessionByID` before building gives a clean "session not found" remediation and avoids writing an empty HTML file for an unknown id (mirrors `session describe`).
- Codex review catches that mattered: `Counts.Error` was only tallied in the Bash branch — non-Bash `is_error` (Read/Edit/Grep) underreported in header/sidebar chips; and the template was *replacing* the Agent dispatch row with the child block instead of rendering the dispatch then nesting the child.
