# Hotfix

Create and release a hotfix from main. Requires NAME argument (e.g. `/hotfix lock-renewal-crash`).

## Steps

1. `git checkout main && git pull origin main`
2. `git checkout -b hotfix/$ARGUMENTS`
3. Fix the specific issue minimally — add tests, update CHANGELOG under `[Unreleased]`
4. Verify: `go test ./... && go build ./... && go vet ./...`
5. Commit with `fix:` prefix and push: `git push -u origin hotfix/$ARGUMENTS`
6. Create PR to main
7. **APPROVAL REQUIRED** — merge PR: `gh pr merge --merge`
8. `git checkout main && git pull origin main`
9. Determine patch version (increment patch from latest tag)
10. **APPROVAL REQUIRED** — create tag: `git tag vX.Y.Z`
11. **APPROVAL REQUIRED** — push tag and create GitHub release
12. Merge main back to develop (preserve `[Unreleased]` section in CHANGELOG)

## Critical Rules

- Hotfixes branch from main, not develop
- Restrict changes to the specific reported issue only
- Three approval gates (merge, tag, push) — cannot be skipped
- When merging main to develop: if develop has `[Unreleased]` entries, preserve them and only add the hotfix version section to main's CHANGELOG

## Reference

@../docs/procedures/release.md
