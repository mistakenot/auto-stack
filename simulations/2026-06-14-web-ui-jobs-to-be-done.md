# User Simulation: auto-ui web dashboard — jobs to be done
*2026-06-14 — 7 simulants — product: local read-only planning-docs explorer for the auto-stack*

**Diversity axis:** Primary = **job-to-be-done** (architect a feature / respond to plan comments / drive a large epic / track work items / onboard / evaluate-adopt / adjacent-domain doc review). Layered orthogonally with **adoption lifecycle** (newcomer → veteran → evaluator → casual) and **time horizon** (today / +3mo / +12mo). Axis confirmed by the user before fan-out.

**Headline result:** Of 7 sessions, **0 succeeded, 2 were partial, 5 were abandoned.** The dashboard is excellent at *"read a doc I already know I want"* and fails at nearly every job that requires triage, status, cross-doc search, comment handling, or capturing output. Five of seven users fell back to the terminal, their IDE, grep, or Google Docs — the exact habits the dashboard is meant to replace.

## Cohort

| # | Name | Job-to-be-done (primary) | Lifecycle | Horizon | Temperament | Perturbation | Outcome |
|---|------|--------------------------|-----------|---------|-------------|--------------|---------|
| 1 | **Maya** | Architect a new auto-* feature from prior-art docs | Veteran / lead user | +12mo | Tinkerer | 2am, deep flow, wants 4 docs side-by-side | partial |
| 2 | **Daniel** | Read & resolve review comments on his solution.md | New adopter | Today | By-the-book | 90 sec before standup | abandoned |
| 3 | **Priya** | Track status of a multi-phase epic | Veteran | +3mo | Perfectionist | Live meeting, boss asking "where are we?" | abandoned |
| 4 | **Sam** | Reorient: what's open / next / stalled | Casual | Today | Impatient | Train, flaky wifi | abandoned |
| 5 | **Lin** | Onboard to an unfamiliar project | Newcomer | Today | Explorer | **Screen-reader user** | abandoned |
| 6 | **Marcus** | Decide: replace Linear + Notion? | Evaluator / skeptic | Today | Skeptical | Migrating, grumpy, checklist | abandoned |
| 7 | **Yuki** | Draft release notes + spot terminology drift | Adjacent-domain (docs/UX) | +3mo | Shortcut-seeker | **Colorblind, 13" screen** | partial |

## Top recommendations

### #1 — Recent-activity / "what changed" feed in the main UI
**Need:** Know what has changed recently — across days away, during a meeting, or live — without staring at one open doc.
**Convergence:** 4/7 — Priya, Sam, Yuki, Lin (Maya brushed it too).
**Kano:** table-stakes (it's the headline "live" promise, just unreachable).
**Horizon:** today.
**Why this is the strongest signal:** Four simulants *independently navigated to the Debug page's event log* and said, in their own words, "this is exactly what I wanted — why is it buried in diagnostics?" That is textbook convergence: separate users discovering the same latent feature by need. The bus already emits `doc.changed`; the data exists. Three sub-needs surfaced: (a) a persistent, non-color, glanceable "recent changes" list in the main UI; (b) **backfill/replay** of what changed while you were away (the live log only shows events since connect — Sam); (c) the flash must survive being looked-away-from and being colorblind (Yuki couldn't see the ~1s green flash at all).
**Evidence:**
- Priya: *"the bus is delivering exactly the signal I need… and the UI drops it on the floor. That event is gold for 'what's being worked right now,' and it's invisible unless you happen to be staring at the debug log."*
- Sam: *"the live-event log only shows events since I connected — it doesn't replay what happened while I was away."*
- Yuki: *"I genuinely cannot rely on the nav flash; the Debug log is my real 'what changed' feed. That's backwards."*

### #2 — Status & triage surface (per-task status, epic rollup, landing summary)
**Need:** See at a glance what's open / in-progress / done / stalled — per task and rolled up per epic — without opening and reading every folder.
**Convergence:** 4/7 — Sam, Priya, Marcus (all **blocker**), Yuki (recent/changelog).
**Kano:** table-stakes.
**Horizon:** today.
**Detail:** Every task folder looks identical in the tree (`NNN-slug` → five identical filenames); nothing conveys lifecycle. Priya's epic showed a *hand-typed* status table that contradicted live activity — status must be **derived from ground truth** (sub-task frontmatter `status:` + recent bus events), not prose. Sam and Lin both wanted a triage **landing page** ("X open, Y stalled, last edited…") instead of being dropped straight into a file tree.
**Evidence:**
- Sam: *"I can read any doc beautifully — I just can't tell which one I left bleeding on the floor three days ago."*
- Priya: *"The dashboard can show me the epic's story but not its state."*
- Marcus: *"Linear gives me status at a glance across 200 issues. Here I'd have to open each folder and read prose to know if it's done."*

### #3 — Cross-doc / full-text search
**Need:** Find a term, a task, or a topic across the whole `docs/` tree.
**Convergence:** 4/7 — Yuki (**blocker**), Sam, Lin, Marcus.
**Kano:** table-stakes for any doc tool.
**Horizon:** today.
**Detail:** There is no in-UI content search (where a search box exists it matched filenames only). Yuki's *entire* terminology-audit goal was undoable in-tool — she'd have had to open 24 folders × 5 files and Cmd-F each. The `auto search` / `auto doc search` capability exists in the CLI but isn't surfaced in the dashboard, splitting the experience.
**Evidence:**
- Yuki: *"to find every place 'autosearch' appears I'd have to open every file and Cmd-F each one. That's 24 task folders × 5 files. No."*
- Sam: *"typed 'todo', got nothing useful."*

### #4 — Comments as a first-class feature (list, count badge, reply/resolve) + render hidden threads
**Need:** Find, count, and act on review comments — instead of eyeballing prose and trusting a renderer that hides some.
**Convergence:** 4/7 — Daniel (two **blockers**), Maya, Marcus (**blocker**), Yuki.
**Kano:** table-stakes + a correctness bugfix.
**Horizon:** today.
**Detail:** Comments are a markdown *convention* (`REVIEW:` / `AUTHOR:` / `<!-- RESOLVED(P1): … -->`), not a feature. Critically, the markdown renderer **silently drops the `<!-- … -->`-wrapped threads**, so the rendered view *undercounts* comments — Daniel's dashboard said "2," grep said "5." Users want: a thread panel with open/resolved status + jump-to, an unresolved-count badge on tree nodes, and in-UI reply/resolve (write-back over the existing bus).
**Evidence:**
- Daniel: *"The dashboard told me two; grep told me five. I'm not walking into standup trusting the one that hides the comments it doesn't like."*
- Marcus: *"'comments' are a file convention, not a feature. My PMs can't @-mention anyone here."*

### #5 — Accessibility foundations (headings, landmarks, labels, aria-live, non-color cues)
**Need:** Perceive and navigate the app with assistive tech and without relying on color.
**Convergence:** 2/7 — Lin (multiple **blockers**, abandoned), Yuki (color-only flash, **major**) — but absence caused outright abandonment, which weights it up.
**Kano:** table-stakes (absence → abandonment).
**Horizon:** today.
**Detail:** Lin found *no headings, no landmarks*, an unlabeled project combo box, silent doc loads, identical filename leaves, and a live-flash feature completely silent to a screen reader — she abandoned and messaged a senior (the thing onboarding should prevent). Yuki, colorblind, literally could not see the green flash. Most fixes are cheap: real heading structure, ARIA landmarks/labels, an `aria-live` region for doc loads and updates, focus management on open, and a non-color change indicator.
**Evidence:**
- Lin: *"the dashboard never told me where to start, and half the time it changed under me without saying a word."*
- Yuki: *"the one 'live' thing it brags about is a green flash I can't even see."*

## Wildcards

- **Drafting / capture surface beside the read docs (Maya, blocker for her).** A scratchpad or "draft doc" pane so an architect can read 4 prior-art docs *and* write her emerging design without leaving — turning the viewer into a workbench. *"It's the best doc finder I've ever had and a terrible drafting table."* Unique to the +12mo lead user, but points at the product's biggest latent expansion.
- **Multi-user / hosted / permissions (Marcus, strategic).** Only one simulant raised it, but it *was* his entire NO-GO verdict: localhost-bound, single-user, no auth, not reachable from a phone. A clear fork in product positioning — "personal cockpit" vs "team tool." *"my team needs to assign it, track it, comment on it, and see it from their phones, and this does none of that."*
- **Copy-as-markdown / clean export (Yuki + Marcus).** Read-only implicitly invites copy-out, but copying flattens rendered HTML (loses paragraphs/lists) and there's no export. A "copy as markdown" button + CSV/status export would unblock the doc-author and the reporting-manager.
- **Plain-language glossary for engineer shorthand (Yuki).** The non-engineer was scared to mistranslate "AC-4" / "doc.changed" into release notes. A hover-glossary or auto-summary would let adjacent-domain users self-serve.
- **Split / multi-pane + collapsible nav (Maya + Yuki).** Two users on opposite ends (2am power user, 13" cramped writer) both needed to see more than one doc / reclaim width. Currently the only way to compare is four full copies of the app across OS windows.

## Friction log (exists today, hurts today — ranked quick wins)

1. **Renderer hides `<!-- … -->` comment threads → comment count is silently wrong** (Daniel). Correctness bug; the dashboard actively misinforms.
2. ~~**`doc.list` doesn't recurse `docs/epics/phase1/` → sub-task docs are invisible AND unreachable** (Priya).~~ **VERIFIED FALSE POSITIVE (2026-06-14).** Code check: the server (`auto-ui/internal/server/docs.go:124`, `filepath.WalkDir` with no skip-list) recurses the full `docs/` tree and returns all 6 `phase1/*.md` files; the client (`auto-ui/web/static/tree.js:119`) groups `epics/phase*/` into an `Epics → phase1` subgroup; `flashTokensForPath` emits `sub:Epics/phase1` so live edits flash too. Replicated against the real repo — all 6 phase1 docs list and group correctly. Priya's "doesn't recurse / flash dropped" was a Wizard-of-Oz hallucination, not a real bug. *(Real adjacent nit found during verification: leaves within a subgroup aren't sorted — `tree.js:169` keeps walk order — so phase docs render as `README,1.4,1.6,1.5,…` instead of `1.2…1.6,README`. Cosmetic; left unfixed by user decision.)*
3. **Live flash is color-only and ~1s transient** → invisible to colorblind users and anyone who glanced away (Yuki, Lin).
4. **Live refresh resets scroll to top mid-read** (Maya). Jarring; punishes the live feature's own users.
5. **Tree shows filenames only — no task title, no status, no last-modified** (Sam, Lin, Marcus). Every triage job dies here.
6. **No headings / no landmarks / unlabeled controls** (Lin). Total structural blindness for assistive tech.
7. **Copying rendered markdown loses paragraph/list structure** (Yuki). Breaks the copy-out workflow read-only implies.
8. **Fixed ~280px nav, not collapsible/draggable; not responsive** (Yuki). Wastes half a 13" screen.
9. **Only explores `docs/` — ignores repo-root `todo.md`** where real TODOs live (Sam).
10. **Adding a project means hand-editing `~/.auto/projects.json`** (Marcus). No in-app action.
11. **The Debug event log is the only working "recent changes" + the only working live region** (Priya, Sam, Yuki, Lin) — exactly inverted priorities.

## Churn signals

- **Lin (newcomer, day one) — abandoned.** No "Start Here," no headings/landmarks, silent updates; ended up messaging a senior — the precise failure onboarding exists to prevent. *Highest-stakes churn: first impression.*
- **Marcus (evaluator) — abandoned / NO-GO.** Bounced off read-only + no work-tracking + no collaboration + single-user within ~13 turns. The adoption decision is lost at "no status field, no assignee."
- **Sam (casual) — abandoned at ~4 min.** `git status` + `todo.md` beat the tool for triage. Casual users have the least patience and the most fallback options.
- **Daniel (new adopter) — abandoned the task.** grep beat the dashboard *and the dashboard's count was wrong*; he resolved comments entirely outside the product.
- **Priya (veteran) — abandoned mid-meeting.** Switched the projector to a terminal (`ls -t` + `git log`) because the sub-task docs were unreachable and status untrustworthy.
- **Maya (veteran) — partial.** Got prior art, then returned to her IDE to author — the habit she came to break.
- **Yuki (adjacent) — partial.** Half the release notes done by hand-transcription; terminology audit impossible in-tool; finished in Google Docs.

---

## Appendix: full traces

### Simulant report: Maya
**Persona:** Staff fintech engineer, veteran auto-stack dogfooder, architecting a brand-new auto-* tool at 2am and needs prior art side-by-side while she drafts.
**Outcome:** partial — assembled prior art and read it comfortably, but the read-only single-pane design fought her the moment she tried to keep multiple docs in view and capture her own design. Ended back in her IDE — the habit she came to break.

**Trace**
1. Opened `auto ui`; project switcher already on `auto-stack`; explorer loaded fast.
2. Opened `docs/auto-package-patterns.md` (under undifferentiated "root docs"); rendered inline, frontmatter as tags.
3. Expanded Tasks (10 newest shown); opened `023-reflect-miner-queue/solution.md`.
4. Clicked back toward patterns — the single reading pane *replaced* 023, losing scroll position.
5. Copied the doc URL, opened it in a second browser tab — hash deep-link worked.
6. Opened 4 tabs (patterns, 023, 019, epic 001); each is a *full* dashboard just to show one doc. Heavy.
7. [INVENTED] Looked for "open in split"/pin on the doc header — nothing; tried `\` — nothing.
8. [INVENTED] Ctrl-clicked two tree files for side-by-side — only last click registered.
9. Arranged 4 tabs as 2×2 OS windows; fan spun up; one tab threw "reconnecting…".
10. Background agent edited epic 001 — live refresh within ~1s (great) but yanked scroll to top mid-paragraph.
11. [INVENTED] Looked for "New doc"/"+" to scaffold `024/solution.md` — read-only; right-click gave nothing.
12. [INVENTED] Selected a paragraph expecting a comment popover — selection inert.
13. Opened Debug; saw the `doc.changed` event, UI state; inferred read-only RPC set (`doc.list`/`doc.get`, no write).
14. [INVENTED] Tried `#/scratch` for an ephemeral notes pane — "doc not found."
15. [INVENTED] Tried `#/compare?docs=…` workspace — ignored, single-doc fallback.
16. Conceded; authored `024/solution.md` in her IDE, dashboard tabs as read-only reference.

**Wish list**
- Split / multi-pane doc view (2–4 docs in one window) — blocker — steps 4–9.
- Pin / tab strip *within* the dashboard (shared nav, not 4 SPAs) — major — step 6.
- Preserve scroll position on live refresh — major — step 10.
- Capture surface: scratchpad / notes / "draft doc" beside read docs — blocker — steps 11–14.
- Add inline comments/highlights from the UI — major — step 12.
- Minimal write RPC (`doc.write`/scaffold-task) behind the bus — nice-to-have — step 13.
- Named, reopenable "workspace"/saved doc-set — nice-to-have — step 15.

**Friction log**
- One reading pane; opening a doc evicts the current one and loses scroll (4, 10).
- The most-referenced blueprint sits in undifferentiated "root docs," no pin/favorite (2).
- Each deep-link opens a full dashboard instance, not a cheap doc view (5–6).
- No context menu / multi-select in the tree (8).
- Four dashboard tabs are heavy and surfaced a websocket reconnect blip (9).

**Invented features used**
- In-doc split / "open in split" — did NOT exist.
- Nav-tree multi-select for side-by-side — did NOT exist.
- New-doc / context-menu scaffolding — did NOT exist (read-only).
- Select-to-comment / highlight popover — did NOT exist.
- `#/scratch` route — did NOT exist.
- `#/compare?docs=…` route — did NOT exist.

**Quote**
*"It's the best doc finder I've ever had and a terrible drafting table — at 2am I just want four docs pinned side by side and somewhere to type, and instead I'm running four copies of a viewer across my monitors and authoring in my IDE anyway."*

---

### Simulant report: Daniel
**Persona:** New backend dev (3 weeks in); by-the-book; needs to find and resolve a senior's inline review comments on his `solution.md` — 90 seconds before standup.
**Outcome:** abandoned — got a partial, untrustworthy count; realized the read-only UI hides HTML-comment threads and offers no resolve mechanism; fell back to grep/CLI.

**Trace**
1. Opened `auto ui`; doc explorer; auto-stack pre-selected.
2. Expanded Tasks; opened his task → `solution.md`; rendered inline.
3. Scrolled; found `REVIEW:` / `AUTHOR:` blocks rendered as plain prose — no threads, no resolve.
4. Kept scrolling a long doc, eyeball-matching a keyword — feared missing one.
5. Ctrl-F "REVIEW" → 2 hits. Senior said "a few"; felt low.
6. Looked for an in-app comment count / filter on the doc metadata — nothing.
7. [INVENTED] Looked for a "Comments" panel / review mode — not there; pane is pure render, read-only.
8. Went to Debug hoping for review events — only websocket + doc.changed; wrong tool.
9. Dropped to terminal: `grep -n "REVIEW:"` = 2; `grep "RESOLVED\|<!--"` revealed **3 more threads hidden in `<!-- RESOLVED(P1): … -->` HTML comments** the renderer dropped.
10. Counted: 5 total, 3 resolved, 2 open — got his standup number from grep, not the dashboard.
11. [INVENTED] Tried `auto doc comments list --status open` — command not found.
12. Standup hit; reported "2 open"; planned to edit markdown by hand outside the product. Abandoned.

**Wish list**
- Comment/review panel listing every thread (location, reviewer, open/resolved, jump-to) — blocker — 6/7.
- Render `<!-- … -->`-wrapped threads instead of hiding them — blocker — 9.
- Unresolved-comment count badge on tree nodes — major — 5/10.
- In-UI reply + mark-resolved (write-back) — major — 7.
- Project-wide "open comments" inbox — nice-to-have.

**Friction log**
- Renderer hides `<!-- … -->` threads → rendered view undercounts comments (most dangerous).
- No comment affordance anywhere; `REVIEW:`/`AUTHOR:` are just prose.
- Read-only: the entire respond/resolve half happens outside the product.
- Browser Ctrl-F only finds rendered text (misses hidden threads) → false confidence.
- Debug page was the only "more info" surface and was irrelevant.

**Invented features used**
- Comments/review panel — did not exist.
- `auto doc comments list --status open` — did not exist.
- In-pane reply/resolve — did not exist.

**Quote**
*"The dashboard told me two; grep told me five. I'm not walking into standup trusting the one that hides the comments it doesn't like."*

---

### Simulant report: Priya
**Persona:** Veteran eng lead driving a multi-phase epic, perfectionist, needs trustworthy at-a-glance status live in a meeting.
**Outcome:** abandoned — the dashboard never ingested `docs/epics/phase1/` sub-task docs (walker doesn't recurse the nested folder), so they're absent from the tree AND `doc.list`. No way to open sub-tasks, no rollup, and live `doc.changed` for them silently dropped; fell back to `ls -t` + `git log`.

**Trace**
1. Opened dashboard on projector; auto-stack selected; tree + empty pane.
2. Clicked Epics — only `001-…md` and `002-…md` flat; no `phase1/` subgroup.
3. Confirmed no nested phase1 folder.
4. Opened epic 001; frontmatter `status: in-progress`; wall of prose with a phase table partway down.
5. Read the hand-maintained table (1.2 done, 1.3 done, 1.4 in progress, 1.5/1.6 not started) — distrusted it as stale.
6. Scanned whole tree for phase1 docs — absent everywhere.
7. [INVENTED] Nav search `phase1` — matched only root docs mentioning the string, never the actual files.
8. Switched projects and back to force a re-walk — identical; structural, not stale cache.
9. Searched `orchestration` for the phase1 README — resolved to epic 001; README node doesn't exist.
10. Opened Debug → "current UI state" doc list — phase1 files absent from `doc.list` data; confirmed data-layer bug.
11. Watched live event log ~30s; a real `doc.changed` for `phase1/1.5-reader-api.md` arrived — tree flashed nothing, open doc didn't refresh.
12. Boss asked directly; read the stale table aloud with a verbal hedge.
13. [INVENTED] Looked for an epic status rollup aggregating child states — doesn't exist.
14. Gave up; switched projector to terminal; `ls -t` + `git log` for true status.

**Wish list**
- Recurse `docs/` fully into tree + `doc.list` (nested subfolders like `epics/phase1/`) — blocker — 2/6/10.
- Epic status board / rollup (per-child `status:` frontmatter, last-modified, last `doc.changed`, color-coded) — blocker — 13.
- Derive status from ground truth, not hand-typed tables — major — 5/11.
- Surface `doc.changed` as a "recent activity" feed in the main UI (not just Debug) — major — 11.
- Flash should fire even when the changed file isn't yet in the tree (auto-insert it) — major — 11.
- "Show full path / open in editor / open enclosing folder" for unreachable docs — nice-to-have — 7.

**Friction log**
- Epics group lists epic `.md` flat with no hint a `phase1/` companion folder exists.
- Nav search matched body text but couldn't locate on-disk files absent from the tree model — misleading.
- Project switch/reload re-rendered an identical incomplete tree, hiding that it's structural.
- The only place the true inventory was visible was Debug's raw state blob — and even there phase1 was missing.
- Live event log buried in Debug; headline "live updates" silently no-ops for any file outside the tree.

**Invented features used**
- Nav tree text filter/search — partially (filtered) but exposed it can't find files absent from the model.
- Epic status rollup/board — did NOT exist; its absence ended the session.

**Quote**
*"The dashboard can show me the epic's story but not its state — and it can't even open the sub-tasks the epic is made of. In a live meeting that's worse than `git log`."*

---

### Simulant report: Sam
**Persona:** Solo indie dev, casual user, back after a few days, wants open/next/stalled in under two minutes — on a train with flaky wifi.
**Outcome:** abandoned — got a partial picture of open tasks but never reliably learned what was *stalled* or *next*; read-only tree + dropping websocket made it slower than just opening `todo.md`.

**Trace**
1. Opened `localhost:7777`; SPA shell fast; connection dot spinning grey.
2. Project switcher loaded after a beat; picked auto-stack.
3. Tree spun ~6s (reading whole `docs/` over bad wifi); rendered Tasks + others.
4. Expanded Tasks: 10 newest (023…014) + "show more"; every folder identical `NNN-slug`, no status.
5. Opened 023 — no feedback.md; guessed "not finished" — a guess, not an answer.
6. Opened 022 — squinted to tell if feedback.md exists; tree shows filenames, not "done."
7. [INVENTED] Searched "todo" — filtered filenames only, matched nothing (no full-text search).
8. Looked for `todo.md` under root docs — not there; dashboard only explores `docs/`.
9. Connection dot went red: "Live updates disconnected" (dead zone); distrusted tree freshness.
10. Opened 023 requirements.md — long; no `status:` frontmatter; doesn't say if it shipped.
11. Opened epic 001 — multi-phase; checkboxes are rendered text, no rollup.
12. Reloaded (~9s); reconnected then amber; no "recently edited" sort anywhere.
13. [INVENTED] Debug page as a "recent activity" proxy — only websocket status + sparse post-connect log.
14. Live log only shows events since connect — no replay of the days away.
15. "Show more" → all ~23 folders, long scroll, still no status/recency; eyes glazed.
16. ~4 min in, still no confident "stalled" answer.
17. Gave up; `git status` + `todo.md` answered in 5s, offline. Closed tab.

**Wish list**
- Task status at a glance in the tree (open/in-progress/done/stalled) — blocker — 4–6, 10–11.
- "Recently modified" / "last activity" view or sort — blocker — 12, 14.
- Landing/triage summary page instead of straight-to-tree — major — 16.
- Surface `todo.md` and root-level scratch files — major — 8.
- Full-text doc search — major — 7.
- Event-log replay / "what changed while you were away" — major — 13–14.
- Offline resilience / cached last-good tree + "may be stale" banner — nice-to-have — 9, 12.

**Friction log**
- Tree shows filenames only, zero status or recency — fatal for triage (4–6).
- Search box filters filenames, not content; silently empty (7).
- Only explores `docs/`; ignores repo-root `todo.md` (8).
- Connection dot churns grey→red→amber with no "reading still works" reassurance (1, 9, 12).
- Reloads slow on bad wifi (~9s), refetch whole tree (12).
- Debug live log only shows post-connect events — no backfill (13–14).
- "Show more" dumps full unsorted-by-recency list (15).

**Invented features used**
- Full-text/TODO search via nav box — did NOT deliver (filename-only).
- Debug log as "recent activity" proxy — did NOT deliver (post-connect only).
- "Recently modified" sort/view — did NOT deliver (doesn't exist).

**Quote**
*"I can read any doc beautifully — I just can't tell which one I left bleeding on the floor three days ago."*

---

### Simulant report: Lin
**Persona:** Junior dev, first day, zero context, screen-reader user — wants a "here's what this is + read this first" mental model.
**Outcome:** abandoned — built a shaky model that it's "docs about agent tooling," never got an authoritative reading order; gave up after no "start here," no headings, and silent live-flash/iframe content.

**Trace**
1. Ran `auto ui`; opened the printed localhost URL.
2. Page loaded; SR announced "auto-ui" then silence — no h1, no skip link.
3. Pressed `H` (headings) → "no headings."
4. Pressed `D` (landmarks) → "no landmarks"; tab order is the only map.
5. Tabbed → unlabeled "combo box, collapsed."
6. Opened it; read "auto-stack", "demo-app", "auto-stack" (a dup?); picked auto-stack.
7. Focus changed but nothing announced; no confirmation docs loaded.
8. Tabbed into "tree" → dropped on leaf "context.md" with no group/category context.
9. Arrowed: "requirements.md, solution.md, plan.md, feedback.md, 023, 022…" — identical filenames, no idea what any task is.
10. [INVENTED] Looked for a search/"start here" field — none in the explorer.
11. Opened root doc "user-journey.md"; pane updated but SR silent; focus stayed in tree.
12. Brute-force tabbed to the pane; found text "End-to-end walkthrough of the Auto stack…" — but no "document loaded" announce, no heading.
13. Frontmatter tags read as unlabeled run-on text "active, planning, 2026…".
14. An agent edited a doc; the tree flashed — SR announced NOTHING; can't tell what changed or trust freshness.
15. Tabbed into an HTML iframe doc (epic 002): "frame" then raw content; mermaid as "graphic" no alt; threads undifferentiated. Impenetrable.
16. [INVENTED] Typed `localhost:7777/#debug` — websocket view; ironically the only working live region.
17. Looked for a README "start here" node — none surfaced.
18. Reconstructed a vague model from user-journey alone; closed tab; messaged a senior — the thing she came to avoid.

**Wish list**
- Default "Start Here"/onboarding landing (overview + ordered reading path) — blocker — 2/10/17.
- Real heading structure (h1 title, h2 per category, h3 on open doc) — blocker — 3.
- ARIA landmarks (`nav`, `main`, `banner`) — blocker — 4.
- Labeled project switcher + announced selection change — major — 5–7.
- Polite `aria-live` region for doc loads + live-update flashes — blocker — 11, 14.
- Move focus to / skip-link to the reading pane on open — major — 12.
- Task tree nodes labeled with task TITLE not filename — major — 8–9.
- Docs search / "what should I read?" box — major — 10.
- Accessible iframe planning docs (semantic tabs, mermaid alt, marked threads) — major — 15.
- Frontmatter tags as a labeled list — nice-to-have — 13.

**Friction log**
- No page heading or skip-link on load; SR silent (2).
- Zero headings and zero landmarks app-wide (3–4).
- Unlabeled combo box; duplicate-looking entries; no announced state change (5–7).
- Tree drops focus on a leaf with no category context (8).
- Identical filenames across dozens of folders, no titles (9).
- Opening a doc doesn't move focus or announce; pane unlabeled (11–12).
- Flagship live-update feature is completely silent to assistive tech (14).
- iframe "rich" docs expose raw, un-semantic content (15).
- Debug page is the only working live region — inverse of need (16).
- No README/"start here"; no reading order anywhere (17–18).

**Invented features used**
- Type-to-filter / docs search in explorer — did NOT deliver.
- `/#debug` direct URL — delivered (only working live region) but irrelevant to onboarding.
- "Start Here" landing view — did NOT deliver; the core gap.

**Quote**
*"They said 'it's all in the dashboard' — but the dashboard never told me where to start, and half the time it changed under me without saying a word. First day, and I'm messaging a senior anyway."*

---

### Simulant report: Marcus
**Persona:** Skeptical eng manager (30-person co.) evaluating auto-stack's dashboard as a Linear + Notion replacement, ticking a ruthless checklist.
**Outcome:** abandoned — **NO-GO for replacement, weak NOT-YET for complement.** A beautiful repo-tied read-only doc viewer with zero work-tracking, collaboration, or multi-user story.

**Trace**
1. Opened `auto ui`; looked for a board / "My Issues" — only a doc tree. It's a docs tool first.
2. Expanded Tasks → folders (001…026), newest-first, capped 10; opened 023 → five files. Notion-page-per-task, but no status.
3. Hunted for a status field (Todo/In Progress/Done) — none; frontmatter chips carry no lifecycle. **status — FAIL.**
4. [INVENTED] Looked for assignee/owner — no user model at all. **assignees — FAIL.**
5. Tried to mark 023 done / edit — pane is flatly read-only. **editing/create/delete — FAIL.**
6. Saw inline comment threads; [INVENTED] tried to reply — inert committed markdown. **comments/@mentions — FAIL.**
7. Project switcher has only auto-stack; adding one means editing `~/.auto/projects.json` on the host.
8. [INVENTED] Tried reaching it from his phone on the LAN — refused; localhost single-user, no auth. **multi-user/permissions/mobile — FAIL×3.**
9. Looked for global search/filter ("everything in progress") — search lives in the CLI, not the UI.
10. Tested live updates: edited 023 on disk → open doc refreshed + tree flashed within ~1s. The one moment he felt the pull.
11. Opened Debug — websocket/event log/state; nice for an engineer, meaningless to PMs.
12. [INVENTED] Looked for export/reporting (burndown, CSV) — none; "export" = read the repo. **export/reporting — FAIL.**
13. Verdict: only ✓s are gorgeous repo-tied reading + best-in-class live refresh. Non-starter as a replacement; at most a personal side monitor. Closed tab.

**Wish list**
- Issue/work-item model with status field per task — blocker — 3.
- Users + assignees — blocker — 4.
- Editing / create / mutate from the UI — blocker — 5.
- Live interactive comments with @mentions — blocker — 6.
- Multi-user access + permissions + shareable/hosted mode — blocker — 8.
- Board/list view with filters (status, owner, label) — major — 9.
- Reporting/rollup + export (CSV, burndown) — major — 12.
- Mobile/responsive access — nice-to-have — 8.

**Friction log**
- Tasks are folders-of-markdown with no surfaced lifecycle; triage requires opening each (2–3).
- Frontmatter chips tease structure they don't deliver (3).
- Inline comment threads look like collaboration but are inert text (6).
- Adding a project = hand-editing host JSON (7).
- Search/filter exists in the CLI but isn't surfaced in the dashboard (9).

**Invented features used**
- Assignee dropdown — did NOT deliver (no user model).
- Reply box on a comment thread — did NOT deliver.
- Reach dashboard from phone on LAN — did NOT deliver (localhost-bound).
- Export button on Tasks — did NOT deliver.

**Quote**
*"It's a stunning little window into how the robots plan their work — but my team needs to assign it, track it, comment on it, and see it from their phones, and this does none of that."*

---

### Simulant report: Yuki
**Persona:** Colorblind (deuteranopia) technical writer/designer on a 13" laptop, non-engineer, drafting release notes and hunting terminology inconsistencies — a shortcut-seeker who lives in Google Docs.
**Outcome:** partial — assembled a rough "what shipped" list and caught some terminology drift, but only by fighting the layout, squinting at flash cues she couldn't see, and copying text out one painful block at a time. Finished in Google Docs.

**Trace**
1. Opened on a 13" screen; fixed ~280px nav + narrow wrapped reading column; divider wouldn't drag.
2. Picked auto-stack; tree populated (Tasks newest-first + others). Whole `docs/` without cloning — the draw.
3. Tasks newest-first capped 10 — recent work on top; opened 026 folder → five files.
4. Opened 026 feedback.md — led with "AC-4 verdict…" engineer shorthand; can't put in release notes; anxiety.
5. Opened 026 requirements.md — human-readable; selected a paragraph, Cmd-C → pasted as one run-on line, lost paragraph/list structure.
6. Tried selecting just the bullet list — finicky in the cramped column; [INVENTED] looked for "copy as markdown" — none.
7. Looked for export / copy-doc / right-click menu — nothing; read-only is copy-hostile.
8. Opened 025/024 requirements one at a time (no side-by-side, no room on 13"); rebuilt the changelog in her head.
9. An agent edited a doc — tree flashed a faint green/grey she couldn't distinguish, gone in ~1s; couldn't tell which file.
10. Hunted for a log; found Debug → event log with a `doc.changed` path + timestamp; had to read raw JSON to learn a human fact.
11. Built "what shipped" by hand, alt-tabbing dashboard→Google Docs, retyping cleaned sentences (paste was garbage).
12. Job B (terminology): noticed "autosearch"/"auto search"/"auto-search" variants; wanted cross-doc search; [INVENTED] typed `/` for a palette.
13. No global search; Cmd-F only the current doc; auditing every term across 24×5 files — no.
14. Used group labels as a consistency check by eye ("Tasks" vs "task folder"); caught it manually, not by tooling.
15. Back to Debug — two more `doc.changed` stacked up; the Debug log is her real "what changed" feed. Backwards.
16. ~60% of release notes, ~10% of the audit; the audit needs grep/repo — the thing the dashboard was meant to spare her.
17. Gave up in-tool for job B; screenshotted the variant spellings as evidence; pasted half-cleaned summaries into Google Docs; asked engineering to grep.

**Wish list**
- Copy-as-markdown / copy-clean on each doc — major — 5/6.
- Global search across all docs — blocker (for the audit) — 12/13.
- Collapsible/draggable nav so the reading pane can go full-width — major — 1.
- Non-color, persistent "what changed" indicator (badge + visible recent-changes list in main UI) — major — 9/10/15.
- "Recent activity / changelog" view (recent tasks + one-line summaries) — major — 8/11.
- Side-by-side / second reading pane — nice-to-have — 8.
- Inline comments / suggestions like Google Docs — nice-to-have — 14.
- Plain-language summaries / glossary for engineer shorthand — nice-to-have — 4.

**Friction log**
- Copying rendered markdown loses structure (paragraphs, lists).
- Two-pane layout fixed, not responsive; cramped reading column on a small laptop, nav too wide.
- Flash-on-change is color-only and transient — fails colorblind users and anyone who looks away.
- Websocket/event info live on a Debug page — "what just changed" treated as diagnostics, not a user need.
- Newest-first capped Tasks is good, but the only task summary is its filename.
- No way to act on findings in-tool (no comments/flags/export) — everything funnels to copy-paste.

**Invented features used**
- Copy-as-markdown button — did not exist (copy gave flattened HTML).
- `/` command palette / global doc search — did not exist (per-doc Cmd-F only).
- Right-click "export/copy doc" — did not exist.

**Quote**
*"The dashboard finally lets me read the plans without bugging an engineer — but it's a reading room, not a desk: I can't copy clean, can't search across docs, and the one 'live' thing it brags about is a green flash I can't even see."*
