Can we create evals for our automated coding tooling? How to make these easy for agents themself to write, create and run? Can we run these on a schedule, or when things change? Can they then feedback into `autoreflect` so we can update our coding processes?

If we have a new planning structure in mind: can we run evals on it compared to the old way of doing things? Can we replay previous interactions between user/ai in such a way that we can have another AI pretend to be the user, and see how a feature WOULD have turned out with a different plan structure in place?

One big plan.md vs. requirements.md, solution.md, plan.md?

Team of sub agents vs. single agent in context?

Can we rerun previous end to end coding jobs in N different ways, and observe how they turned out?

Can we auto-generate these scenarios from our code base history?

Can we then use our same tooling to automatically grade these different scenarios against what actually happened? And run them X times to see what comes out the best? What is the statistical minimum number of times we need to run a different scenario to be able to compare it to ground truth?

Look at evals for open source coding agents - codex, gemini, to get some ideas.

Examples of experiments we could run:
- `beads` vs. keep everything in plan file
- team of sub-agents vs 1x main agent
- claude vs codex vs gemini
- how to make it more token efficient?
- how to make it run faster?
- how good are OSS / OpenCode models compared to claude?

Set up for a scenario could consist of:
- git hash for starting state
- set of bash commands to run to modify the repo (remove .beads folder, remove documentation files, modify CLAUDE.md, etc)
- Patch certain files with new content
- optionally, ask an agent to make certain changes, like rewrite the plan.md file or convert beads to json, or whatever
- save / inject artifacts from current code base state for golden data

- Then rerun the implementation stage
- Once complete, grade what happened
- All linked to the branch we're working on.

We want to ask "what if?" and play out different scenarios. Then auto-collect feedback using graders + stats.
What about evals for skills?
Can we eval against different prompting techniques?
Full repo search for context vs targetted vs using autograph?
Impact analysis techniques? which are best?
Give it broken code - ask it to figure out whats wrong and fix it. How good are our tests really?

after complete, write up an EVAL.md and leave in root of worktree / branch / collect together somehow. Or put in the `.auto/eval/results` folder in main branch with cron job.

Problems:
- how to filter / exclude this in etl history? maybe by branch prefix?