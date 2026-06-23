# Feedback: Task 027 — Activity Feed

## Problems faced
1. agent-browser eval output includes surrounding quotes -- JS string results come back as `"value"` not `value`; need `tr -d '"'` in shell scripts that grep the output
2. Sidebar layout refactor -- DocTree renders its own `<aside class="sidebar">` so adding a second component (ActivityFeed) to the same column required wrapping both in a `.sidebar-col` flex container and moving the fixed-width/border-right properties up to the wrapper
3. Conformance driver shell quoting -- compound JS expressions with `const` in agent-browser eval don't always parse cleanly through shell interpolation; simpler inline expressions like `(el || {}).textContent` are more reliable

## Reflections
- The existing `docevents.js` / `rpc.js on()` pattern made the WS subscription trivial — the main work was UI composition and CSS, not plumbing
- The conformance test pattern from tasks 025/026 transferred well; the driver.sh approach (fixture → build → launch → agent-browser assertions) is reusable
- Considered putting the feed inside `tree.js` as a section, but keeping it as a separate module (`activity.js`) with its own state was cleaner and avoids bloating the tree component

## Useful context
- `docevents.js` parseDocChanged is the single source of truth for reading doc.changed — always use `ev.data.path`, never `ev.path`
- The `.sidebar-col` wrapper pattern is now the way to add more sections to the left rail without modifying tree.js internals
