# Release Feature

Merge feature branch to develop with history cleanup. Phases 1–3 of the release workflow. Requires BRANCH argument (e.g. `/release-feature feature/lock-renewal`).

## Steps

### Phase 1: History Cleanup

1. Review commit history since branching from develop:
   ```bash
   git checkout $ARGUMENTS
   git fetch origin develop
   git log --oneline develop..HEAD
   ```
2. Propose a clean commit structure using conventional commits
3. **APPROVAL REQUIRED** — interactive rebase: `git rebase -i $(git merge-base develop HEAD)`
4. Verify after rebase: `go test ./... && go build ./... && go vet ./...`
5. **APPROVAL REQUIRED** — force push: `git push --force-with-lease origin $ARGUMENTS`

### Phase 2: Merge to Develop

1. Rebase onto latest develop:
   ```bash
   git fetch origin develop
   git rebase origin/develop
   git push --force-with-lease origin $ARGUMENTS
   ```
2. Create PR to develop with commit summary
3. Show PR diff and checks
4. **APPROVAL REQUIRED** — merge: `gh pr merge --merge --delete-branch`

### Phase 3: Verify Develop

1. `git checkout develop && git pull origin develop`
2. `go test ./... && go build ./... && go vet ./...`
3. Report result

## Critical Rules

- Approval gates cannot be bypassed
- Show current history before proposing rebase plan
- Show diffs before merging
- Conventional commit format required

## Reference

@../docs/procedures/feature-release.md
