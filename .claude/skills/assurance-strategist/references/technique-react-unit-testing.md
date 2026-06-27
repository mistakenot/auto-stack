---
name: React Unit Testing
summary: Renders a single UI component in a simulated DOM and asserts it displays the right output for given props/state
oracle: exact
archetypes: ui-component, render-surface
criticality-min: C2
volatility-fit: loose
harness: ci
pairs-with: unit-testing, end-to-end-testing
upgrade-looser: none
upgrade-stricter: end-to-end-testing
cost-author: low
cost-maintain: medium
cost-run: fast
---

## What it is & what it catches/misses

React unit testing renders a single component (or a small cluster of collaborating components) into a simulated DOM — jsdom or happy-dom — and asserts on what the user would see: rendered text, roles, form values, conditional sections, and the component's response to interaction. It is unit testing applied to the *view layer*: the unit is a component, the DOM is the output, and the oracle is exact (the author specifies what should render). Crucially, it needs **no running server, no browser, and no dev server** — the DOM shim runs in-process inside the same test runner used for logic tests, so it is Vite/Vitest-compatible by default.

**Catches:** components that crash on render, props that are ignored or mis-mapped to the DOM, conditional rendering bugs (empty states, loading/error branches, count-driven visibility), incorrect formatting at the display boundary (currency, dates, pluralization), broken controlled-input wiring, event handlers that don't fire or pass the wrong payload, and accessibility regressions (missing roles/labels) when queried semantically.

**Misses:** real network/routing behavior, server-side rendering and data-fetching wired through the framework, true cross-page navigation, visual/layout regressions (pixels, CSS, overflow — jsdom has no layout engine), and bugs that only manifest in a real browser engine. For those, graduate to end-to-end or visual-regression testing.

## When to prescribe / when not

**Prescribe when:**
- The project has a UI component layer (React, and the same shape applies to Vue/Svelte/Solid) with display logic worth verifying — formatting, conditional sections, derived props, form state.
- Criticality is C2 or above for the view: a wrong render silently misleads the user (e.g. an expense shown with the wrong amount or a summary card hidden when it should appear).
- You want fast, server-free feedback on the layer the user actually touches — component tests run in milliseconds alongside logic unit tests.
- A fullstack or frontend app would otherwise have its entire UI surface untested because the logic layer absorbed all the attention.

**Do not prescribe when:**
- The component is a pure pass-through with no logic, formatting, or conditional rendering (a styled `<div>` wrapper).
- The behavior under test is genuinely cross-page or server-dependent — that is end-to-end territory, not a component unit.
- The concern is visual/pixel fidelity — jsdom cannot see layout; use visual-regression instead.
- There is no view layer at all (pure library/CLI/backend) — this technique does not apply.

## Prerequisites

**Artifacts:** a component layer with components that take props/state and render DOM — i.e. identifiable view units with clear inputs.

**Infrastructure:** a test runner (Vitest is the natural fit for Vite/Vinxi projects; Jest otherwise), a DOM environment (`jsdom` or `happy-dom` — happy-dom is faster, jsdom is more complete), and a component testing library (`@testing-library/react` + `@testing-library/jest-dom` matchers; `@testing-library/user-event` for interaction). All install as devDependencies and run in-process — **no server, no browser binary, no separate harness**.

## Design decisions

The architect must decide:

- **Query strategy:** prefer semantic, user-facing queries (`getByRole`, `getByLabelText`, `getByText`) over structural ones (`querySelector`, test-ids, class names). Semantic queries survive markup refactors and double as accessibility checks; structural queries are the primary source of component-test brittleness. State this as the default and reserve `data-testid` for cases with no accessible handle.
- **Render boundary:** how much to render. Default to the single component with real children; mock only true leaf dependencies (network calls, router context, heavy third-party widgets). Over-mocking children turns the test into a tautology.
- **DOM environment:** happy-dom (fast, covers most cases) vs jsdom (slower, broader API coverage). Pick one in the runner config and state why.
- **Interaction model:** `user-event` (simulates real user gestures, async) over low-level `fireEvent`. State the default so tests are consistent.
- **Assertion style:** assert on user-observable output (text content, roles, presence/absence, input values) via `jest-dom` matchers (`toBeInTheDocument`, `toHaveTextContent`, `toHaveValue`), not on internal component state or implementation details.

## Derivation guidance

Heuristics the architect embeds so implementing agents can find what to test:

1. **Render-without-crash floor:** every component gets at least one test that renders it with representative props and asserts a key element is present. This alone catches a large class of integration-time crashes.
2. **Prop→DOM mapping:** for each prop that affects output, assert the rendered DOM reflects it (an amount prop renders the formatted amount; a label prop renders the label).
3. **Conditional branches:** every `&&`, ternary, and early-return in JSX is a test case — empty state vs populated, loading/error/success, count-driven visibility (zero-item sections hidden, single vs many).
4. **Formatting boundaries:** wherever the component formats data for display (currency, dates, rounding, pluralization), test the exact rendered string at representative and edge values (e.g. `1210` pence → `£12.10`, not `£12.1`).
5. **Controlled inputs & forms:** assert initial values render, typing updates the field, and submit calls the handler with the correctly-shaped payload (including unit conversions, e.g. pounds→pence).
6. **Interaction outcomes:** for each interactive element, simulate the gesture with `user-event` and assert the observable result (handler called with expected args, or DOM updated).
7. **Accessibility anchor:** because semantic queries are the default, every interactive element is implicitly checked for an accessible role/name — a missing label surfaces as a failing query.

## Minimum viable instance vs full rigor

Choose the rung that matches the four axes; component testing can start as a fast display-logic check and only become a broad render contract when the UI surface justifies it.

**Light / minimum viable (20 minutes):** a render-without-crash test plus prop-to-DOM assertions for the 2-3 components carrying the most display logic (the ones where a wrong render most misleads the user). Runs in the existing Vitest process with a one-line `environment: 'happy-dom'` config change.

**Standard:** cover changed and high-risk components with semantic queries, conditional branches, formatting boundaries, and key interactions via `user-event`. Fold these tests into the normal local verification command and record the runner output. This is the default rung for product UI work.

**Full rigor:** every component has render, prop-mapping, conditional-branch, and formatting-boundary tests; forms have controlled-input and submit-payload tests; interactions are exercised via `user-event`; queries are uniformly semantic; the suite is wired into `make verify` and CI with a kill-test proving detection power.

## Harness changes

| Component | Delta |
|---|---|
| `docs/testing.md` | Add a "React Unit Testing" section: query strategy (semantic-first), render boundary, DOM environment choice, formatting-boundary heuristics |
| Test runner config | Set `test.environment` to `happy-dom` (or `jsdom`); add a setup file importing `@testing-library/jest-dom` so matchers are registered |
| `make verify` | Add a `verify-components` subtarget (e.g. `vitest run tests/components/`) or fold component tests into the existing `verify-unit` run; wire into top-level `make verify` |
| `package.json` | Add devDeps: `@testing-library/react`, `@testing-library/jest-dom`, `@testing-library/user-event`, and `happy-dom` (or `jsdom`) |
| `AGENTS.md` / `CLAUDE.md` | Add line: "Components with display logic require render tests — see docs/testing.md; query by role/text, not by test-id" |
| Evidence conventions | Test runner output (pass/fail counts, failing query diffs) captured in `.evidence/` or PR comment |

## How to get to a walking skeleton

1. **Scaffold:** add the DOM environment + jest-dom setup to the runner config and create `tests/components/`.
2. **Write the fail-fake:** render a trivial component and assert it contains text it does *not* render. Confirm the runner reports failure and `make verify` exits non-zero.
3. **Write the pass-fake:** assert the same component contains text it *does* render. Confirm the green path and that matchers (`toBeInTheDocument`) are wired.
4. **Run locally:** `make verify-components` (or `verify-unit`) goes red with the fail-fake, green with only the pass-fake.
5. **Run via `make verify`:** confirm the component subtarget is composed in and failures propagate.
6. **Confirm CI:** push both fakes, observe CI red; remove the fail-fake, observe CI green; link both runs as evidence.
7. **Remove fakes:** delete them (or park the fail-fake behind a self-test target).

## Acceptance criteria to embed

The architect writes these into the generated artifacts so implementing agents can self-grade:

- [ ] Every component with display logic has at least one render-without-crash test with representative props.
- [ ] Every prop that affects output has an assertion that the rendered DOM reflects it.
- [ ] Every conditional render branch (empty/loading/error/populated, count-driven visibility) has a test.
- [ ] Formatting boundaries (currency, dates, rounding) are asserted on the exact rendered string at edge values.
- [ ] Forms test initial-value rendering, input updates, and submit-handler payload shape.
- [ ] Queries are semantic (`getByRole`/`getByText`/`getByLabelText`); `data-testid` used only where no accessible handle exists.
- [ ] No test asserts on internal component state or implementation details — only user-observable output.
- [ ] `make verify-components` (or the combined unit target) passes locally and in CI.
- [ ] **Kill test:** introduce a deliberate render bug (drop a prop into the DOM, invert a conditional, break a formatter). At least one existing test must fail. Record the mutation and the catching test as evidence.

## Composition

**Upstream feeds:**
- **Unit Testing** covers the pure logic the component consumes (formatters, derived-value helpers) so component tests can focus on rendering and wiring rather than re-verifying arithmetic. Test a formatter once as a unit; assert the component *uses* it once at the boundary.
- **Type-driven assurance** narrows prop types so component tests cover behavior, not type validation.

**Downstream consumers:**
- **End-to-end testing** is the strict upgrade — when behavior depends on real routing, server functions, or a real browser engine, graduate from in-process component render to a full E2E run. Component tests stay as the fast first line; E2E covers the integrated path they cannot see.
- **Visual-regression testing** covers the pixel/layout dimension jsdom is blind to, reusing the same component render setup under a real or headless browser.

## Failure modes & retirement triggers

| Sign | Diagnosis | Correction |
|---|---|---|
| Tests break on every markup tweak without catching bugs | Structural queries (test-ids, class names, `querySelector`) coupled to implementation | Rewrite against semantic queries (role/text/label) |
| Component tests pass but the page is broken | Each component verified in isolation; the integrated/routed page is untested | Add end-to-end tests for the integrated path |
| Suite is slow for a UI layer | jsdom over-used, or full app trees rendered per test | Switch to happy-dom; render the single component with mocked leaf deps |
| Everything mocked, tests always green | Children/dependencies over-mocked — the test asserts on mocks, not the component | Render real children; mock only network/router/heavy leaves |
| Tests assert internal state | Reaching into hooks/state instead of observable output | Re-express assertions in terms of rendered DOM and user-visible behavior |
| "It needs a server" used to skip the UI layer | Misconception — component render runs in-process with a DOM shim, no server | Prescribe this technique; reserve "needs a server" for genuine E2E |

**Retirement triggers:** component tests for a view are candidates for removal when the component is deleted or replaced, or when an E2E/visual suite demonstrably covers the same render assertions with stronger guarantees. Never retire a layer's component tests merely because logic units exist — they cover different bug classes (render/wiring vs computation).

## Tool pointers

- **Runner:** Vitest (Vite/Vinxi-native, shares config with logic tests), Jest (mature, broader ecosystem)
- **DOM environment:** happy-dom (fast, lightweight), jsdom (broader API coverage)
- **Component library:** `@testing-library/react` (+ `/vue`, `/svelte` variants), `@testing-library/jest-dom` (DOM matchers), `@testing-library/user-event` (realistic interaction)
- **Config:** set `test.environment: 'happy-dom'` in `vitest.config.ts`; register matchers in a setup file via `import '@testing-library/jest-dom'`
- **Upgrade paths:** Playwright / Cypress component testing (real browser engine), Playwright/Storybook for visual-regression
