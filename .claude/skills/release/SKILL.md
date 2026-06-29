---
name: release
description: |
    Release develop to main with a version tag and GitHub release. Usage: /release VERSION (e.g. /release v0.5.0)
disable-model-invocation: true
allowed-tools:
  - Bash
  - Read
  - Edit
---

# Release

Release version `$ARGUMENTS` of gopipe-azservicebus.

Read the full procedure before starting: @../docs/procedures/release.md

## Steps

**Phase 1 — Prepare:**
1. Confirm current branch is `develop` and up to date: `git pull origin develop`
2. Run checks: `go test ./... && go build ./... && go vet ./...`
3. Update CHANGELOG.md: move `[Unreleased]` entries to a new `## [VERSION] - DATE` section
4. Commit: `git commit -m "chore: release VERSION"`
5. **PAUSE: ask for approval before proceeding**

**Phase 2 — Merge to main:**
1. Create release PR: `gh pr create --base main --head develop --title "release: VERSION"`
2. Show PR checks: `gh pr checks`
3. **PAUSE: ask for approval before merge**
4. `gh pr merge --merge`

**Phase 3 — Tag and release:**
1. `git checkout main && git pull origin main`
2. **PAUSE: ask for approval before creating tag**
3. `git tag VERSION`
4. **PAUSE: ask for approval before pushing tag**
5. `git push origin VERSION`
6. `gh release create VERSION --title "VERSION" --notes-from-tag`

**Phase 4 — Post-release:**
1. `git checkout develop && git merge main && git push origin develop`
2. Report release URL

## Rules

- Never skip an approval gate
- Always run checks before creating the PR
- Merge back to develop after release
