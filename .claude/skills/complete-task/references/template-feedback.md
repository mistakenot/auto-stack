# Feedback Template

```markdown
# Feedback: Task $ID

## Problems faced
1. $obstacle -- $context_to_understand

## Reflections
- What was tricky?
- What would you tell yourself at the start?
- What did you almost do but didn't?

## Useful context
- $specific_resource_that_was_valuable
- $architecture_decision_that_helped
```

Commit and push feedback before merging:

```bash
git add docs/tasks/$ID-$NAME/feedback.md
git commit -m "add task feedback for $ID"
git push
```
