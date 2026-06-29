# Release Procedures

This document outlines the procedures for releasing new versions of gopipe-azservicebus.

## Version Numbering

Uses [Semantic Versioning](https://semver.org/):

- **Major** (v1.0.0): Breaking public API changes after v1.0
- **Minor** (v0.X.0): New features, breaking changes before v1.0
- **Patch** (v0.0.X): Bug fixes only

Check the latest released version:

```bash
git tag --sort=-version:refname | head -5
```

## Regular Release Process

### 1. Prepare on develop

```bash
git checkout develop
git pull origin develop
go test ./... && go build ./... && go vet ./...
```

Update `CHANGELOG.md`: move all `[Unreleased]` entries into a new versioned section:

```markdown
## [vX.Y.Z] - YYYY-MM-DD

### Added
- ...
```

Commit:

```bash
git commit -am "chore: release vX.Y.Z"
```

### 2. Create PR to main

```bash
gh pr create --base main --head develop --title "release: vX.Y.Z" --body "$(cat <<'EOF'
## Summary

Release vX.Y.Z — see CHANGELOG.md for details.

## Pre-release checklist

- [ ] Tests pass
- [ ] Build succeeds
- [ ] Vet passes
- [ ] CHANGELOG updated
EOF
)"
```

**PAUSE: ask for approval before merging.**

### 3. Merge and tag

```bash
gh pr merge --merge
git checkout main && git pull origin main

git tag vX.Y.Z
git push origin vX.Y.Z

gh release create vX.Y.Z --title "vX.Y.Z" --notes-from-tag
```

### 4. Sync develop

```bash
git checkout develop
git merge main
git push origin develop
```

## Hotfix Release Process

For critical fixes to production that cannot wait for a regular release.

### 1. Create hotfix branch from main

```bash
git checkout main
git pull origin main
git checkout -b hotfix/<descriptive-name>
```

### 2. Fix, test, and commit

```bash
# implement fix
go test ./... && go build ./... && go vet ./...
git commit -am "fix: description of the fix"
git push -u origin hotfix/<descriptive-name>
```

Update `CHANGELOG.md` with the new patch version:

```markdown
## [vX.Y.Z] - YYYY-MM-DD

### Fixed

- Description of the fix
```

### 3. Create PR to main

```bash
gh pr create --base main --title "fix: description" --body "Hotfix for <issue>"
```

**PAUSE: ask for approval before merging.**

### 4. Merge, tag, and release

```bash
gh pr merge --merge
git checkout main && git pull origin main

git tag vX.Y.Z
git push origin vX.Y.Z

gh release create vX.Y.Z --title "vX.Y.Z" --notes "$(cat <<'EOF'
## Fixed

- Description of the fix

## Full Changelog

https://github.com/fxsml/gopipe-azservicebus/compare/vX.Y.Z-1...vX.Y.Z
EOF
)"
```

### 5. Sync develop

```bash
git checkout develop
git merge main
# Resolve any CHANGELOG conflicts: keep [Unreleased] at top, add hotfix version below it
git push origin develop
```

### 6. Clean up

```bash
git branch -d hotfix/<descriptive-name>
git push origin --delete hotfix/<descriptive-name>
```

## Pre-Release Checklist

- [ ] All tests pass: `go test ./...`
- [ ] Build succeeds: `go build ./...`
- [ ] Vet passes: `go vet ./...`
- [ ] CHANGELOG updated with version and date
- [ ] No uncommitted changes: `git status`

## Post-Release Checklist

- [ ] Tag visible on remote: `git ls-remote --tags origin | grep vX.Y.Z`
- [ ] GitHub release visible: `gh release view vX.Y.Z`
- [ ] develop synced with main
