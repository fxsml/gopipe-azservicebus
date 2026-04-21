# Release Procedures

## Regular Release

For planned releases with new features, following git flow: develop → main → tag.

### Pre-Release Checklist

- [ ] `go test -race ./... && go build ./... && go vet ./...` passes
- [ ] CHANGELOG.md updated (move `[Unreleased]` to version section with date)
- [ ] No uncommitted changes
- [ ] ADR statuses updated (Accepted → Implemented)

### Release Steps

1. Update CHANGELOG: move `[Unreleased]` entries to `[vX.Y.Z] - YYYY-MM-DD`
2. Commit to develop: `chore: prepare release vX.Y.Z`
3. Create PR from develop to main titled `release: vX.Y.Z`
4. **APPROVAL REQUIRED** — merge PR: `gh pr merge --merge`
5. `git checkout main && git pull origin main`
6. **APPROVAL REQUIRED** — create tag: `git tag vX.Y.Z`
7. **APPROVAL REQUIRED** — push tag: `git push origin vX.Y.Z`
8. Create GitHub release with CHANGELOG notes
9. Merge main back to develop:
   ```bash
   git checkout develop
   git merge main
   git push origin develop
   ```
10. Verify publication: `go list -m github.com/fxsml/gopipe-azservicebus@vX.Y.Z`

### GitHub Release Notes Template

```markdown
## Changes

<copy from CHANGELOG for this version>

## Full Changelog

https://github.com/fxsml/gopipe-azservicebus/compare/vPREV...vNEW
```

## Hotfix Release

For critical fixes that cannot wait for a regular release.

1. `git checkout main && git pull origin main`
2. `git checkout -b hotfix/<short-description>`
3. Fix minimally — add tests, update CHANGELOG under `[Unreleased]`
4. `go test -race ./... && go build ./... && go vet ./...`
5. `git push -u origin hotfix/<short-description>`
6. Create PR to main
7. **APPROVAL REQUIRED** — merge PR
8. `git checkout main && git pull origin main`
9. Determine patch version (increment patch from latest tag: `git describe --tags --abbrev=0`)
10. **APPROVAL REQUIRED** — `git tag vX.Y.Z`
11. **APPROVAL REQUIRED** — `git push origin vX.Y.Z`
12. Create GitHub release
13. Merge main to develop — if develop has `[Unreleased]` entries, preserve them; add only the hotfix version section

## Versioning

Follow semver: `vMAJOR.MINOR.PATCH`
- **MAJOR**: breaking API changes (constructor signature change, removed types)
- **MINOR**: new features, backward-compatible
- **PATCH**: bug fixes only
