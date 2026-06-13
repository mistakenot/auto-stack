---
hash: "74a206d1"
id: "4a835a58"
read_when: "designing requirement extraction or requirements playbook systems from agent session history"
summary: "Concept for building a reusable requirements playbook by mining past agent sessions to extract standards and rules for fleshing out future task requirements."
title: "Requirements Mining"
---

# Requirements mining

Requirements mining it the task of extracting a set of reusable rules and standards to be used by the AI when fleshing out requirements for future tasks.

## The problem

At the moment the user still needs to spend a lot of time specifying very clear and long requirements when working on a new task. Quite often these requirements contain rules and information that could have been inferred from previous sessions, had the work been done to filter and organise that context.

## The solution

We create a new process whereby an agent can mine previous sessions, requirements, inputs and specifications from the user and merge those into an ever-growing requirements playbook. The requirements playbook is a single file which specifies sort of generalised versions of those previous requirements from other sessions along with clear if, when, then type predicate statements to make it clear when to use them and when not to use them.

## Requirements playbook example

```yaml
rules:
    - id: git-based-end-to-end-testing
      # the rule stated as a direct instruction
      description: Use old git history from this repository (auto-stack) as stable test data for a git based processing pipeline
      # when to use this rule
      use_when: You are creating a more developed feature which is focussed on querying / processing git based data and you need a stable dataset without the complexity of creating a testing git submodule 
      # unless any of these conditions hold:
      except_when: 
        - Feature requires a git repo in a particular state, like specific tags, or in a rebase state, or would otherwise lead to messing / change the state of this git repo 
```

## How to automatically build a playbook?
