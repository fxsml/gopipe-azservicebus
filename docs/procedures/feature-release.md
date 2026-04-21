# Feature Branch Release Procedure

Guides the release of a feature branch through git flow with proper history cleanup.

## Overview

```
feature/xxx → (cleanup) → develop → (verify) → main → (tag)
```

**Approval gates** are required before:
- Every rebase/history rewrite
- Every merge
- Every push to remote
- Every tag creation

## Step-by-Step

### Phase 1: History Cleanup

#### 1.1 Review Current History

```bash
git checkout feature/xxx
git fetch origin develop
git fetch origin main

git log --oneline develop..HEAD
git log --stat develop..HEAD
git rev-list --count develop..HEAD
```

#### 1.2 Plan Commit Structure

Aim for logical, atomic commits using conventional commit types:

| Type | Example |
|------|---------|
| `feat` | `feat(subscriber): add async receive mode` |
| `fix` | `fix(publisher): handle context cancellation before retry` |
| `refactor` | `refactor(semaphore): simplify acquire with timeout` |
| `test` | `test(subscriber): add reconnection integration test` |
| `docs` | `docs: add lock renewal to README` |
| `chore` | `chore: upgrade azure-sdk-for-go to v1.11.0` |

**Target structure:**
1. Core types/interface changes (`feat`)
2. Implementation (`feat`)
3. Tests (`test`)
4. Documentation (`docs`)
5. Bug fixes found during development (`fix`)

#### 1.3 Interactive Rebase

**⚠️ APPROVAL REQUIRED before rebase**

```bash
MERGE_BASE=$(git merge-base develop HEAD)
echo "Merge base: $MERGE_BASE"
git rebase -i $MERGE_BASE
```

#### 1.4 Verify After Rebase

```bash
git log --oneline develop..HEAD
go test ./... && go build ./... && go vet ./...
```

#### 1.5 Force Push

**⚠️ APPROVAL REQUIRED before force push**

```bash
git push --force-with-lease origin feature/xxx
```

### Phase 2: Merge to Develop

#### 2.1 Rebase Onto Latest Develop

```bash
git fetch origin develop
git rebase origin/develop
git push --force-with-lease origin feature/xxx
```

#### 2.2 Create PR to Develop

```bash
gh pr create \
  --base develop \
  --title "feat(scope): short description" \
  --body "$(cat <<'EOF'
## Summary

- What changed
- Why it changed

## Commits

$(git log --oneline develop..HEAD)

## Test Plan

- [ ] `go test ./...` passes
- [ ] Integration tests pass with Azure connection
EOF
)"
```

#### 2.3 Review PR

```bash
gh pr view
gh pr checks
gh pr diff
```

#### 2.4 Merge PR

**⚠️ APPROVAL REQUIRED before merge**

```bash
gh pr merge --merge --delete-branch
```

### Phase 3: Verify Develop

```bash
git checkout develop
git pull origin develop
go test ./... && go build ./... && go vet ./...
```

### Phase 4: Merge to Main

#### 4.1 Create Release PR

```bash
gh pr create \
  --base main \
  --head develop \
  --title "release: vX.Y.Z" \
  --body "$(cat <<'EOF'
## Release vX.Y.Z

See CHANGELOG.md for full details.

## Pre-release Checklist

- [ ] All tests pass
- [ ] CHANGELOG.md updated
- [ ] Documentation updated
EOF
)"
```

#### 4.2 Merge to Main

**⚠️ APPROVAL REQUIRED before merge**

```bash
gh pr merge --merge
```

### Phase 5: Tag and Release

#### 5.1 Update Local Main

```bash
git checkout main
git pull origin main
```

#### 5.2 Create and Push Tag

**⚠️ APPROVAL REQUIRED before tagging and pushing**

```bash
VERSION=vX.Y.Z
git tag $VERSION
git push origin $VERSION
```

#### 5.3 Create GitHub Release

```bash
gh release create $VERSION \
  --title "$VERSION" \
  --notes "$(sed -n "/## \[$VERSION\]/,/## \[/p" CHANGELOG.md | head -n -1)"
```

### Phase 6: Post-Release

```bash
git checkout develop
git merge main
git push origin develop
```

Verify publication:
```bash
go list -m github.com/fxsml/gopipe-azservicebus@$VERSION
```

## Troubleshooting

### Rebase Conflicts

```bash
git status          # See conflicted files
# ... resolve conflicts ...
git add <resolved>
git rebase --continue
# Or abort: git rebase --abort
```

### Tests Fail After Rebase

Fix issues, then:
```bash
git add .
git commit --amend --no-edit
git push --force-with-lease origin feature/xxx
```

### Wrong Commit Message Format

```bash
# Reword last commit
git commit --amend -m "fix(subscriber): correct lock renewal timing"

# Reword older commit
git rebase -i HEAD~N  # change 'pick' to 'reword'
```

## Checklist Summary

### Phase 1: History Cleanup
- [ ] Reviewed commit history
- [ ] Planned commit structure
- [ ] **APPROVED** rebase
- [ ] Rebased to clean history
- [ ] Tests pass after rebase
- [ ] **APPROVED** force push

### Phase 2: Merge to Develop
- [ ] Created PR to develop
- [ ] PR checks pass
- [ ] **APPROVED** merge

### Phase 3: Verify Develop
- [ ] Tests pass on develop

### Phase 4: Merge to Main
- [ ] Created release PR
- [ ] **APPROVED** merge to main

### Phase 5: Tag and Release
- [ ] **APPROVED** tag creation
- [ ] Created tag
- [ ] **APPROVED** tag push
- [ ] Pushed tag
- [ ] Created GitHub release
- [ ] Verified module publication

### Phase 6: Post-Release
- [ ] Merged main to develop
