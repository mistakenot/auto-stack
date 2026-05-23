# PR Body Template

```bash
git push -u origin HEAD
gh pr create --title "feat($ID): $NAME" --body "$(cat <<'EOF'
## Summary
- [1-3 bullets from plan.md summary]

## Phases completed
- [x] Phase 1: name
- [x] Phase 2: name
- [ ] Phase 3: name (failed / skipped)

## Issues encountered
- [Bullet list or "None"]

## How to test
[Copy from plan.md]

## Links
- Task docs: docs/tasks/$ID-$NAME/
- Plan: docs/tasks/$ID-$NAME/plan.md

Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```
