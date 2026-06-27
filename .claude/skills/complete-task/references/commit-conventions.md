# Commit Conventions

Commits capture intent and decisions, not just what changed.

## Subject Line

Standard Conventional Commits format: `type(scope): description`

## Body -- Action Lines

Optional, for significant commits:

```
intent(scope): what user wanted and why
decision(scope): approach chosen when alternatives existed
rejected(scope): what was considered and discarded + reason
constraint(scope): hard limits/dependencies discovered
learned(scope): API quirks, undocumented behaviors
```

## Phase Commits (during execution)

```
feat($ID): phase N - $description

intent(task): $what_this_phase_accomplishes
```

## Plan Tracking Commits

```
docs($ID): mark phase N complete
```
