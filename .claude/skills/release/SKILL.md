# Release

Release develop to main with tag. Phases 4–6 of the release workflow. Requires VERSION argument (e.g. `/release v1.2.0`).

## Steps

### Phase 4: Merge to Main

1. Update CHANGELOG: move `[Unreleased]` entries to `[$VERSION] - YYYY-MM-DD` section
2. Commit changelog update to develop
3. Create PR from develop to main titled `release: $VERSION`
4. **APPROVAL REQUIRED** — merge PR: `gh pr merge --merge`

### Phase 5: Tag and Release

1. `git checkout main && git pull origin main`
2. **APPROVAL REQUIRED** — create tag: `git tag $VERSION`
3. **APPROVAL REQUIRED** — push tag: `git push origin $VERSION`
4. Create GitHub release with notes from CHANGELOG

### Phase 6: Post-Release

1. Merge main back to develop:
   ```bash
   git checkout develop
   git merge main
   git push origin develop
   ```
2. Verify: `go list -m github.com/fxsml/gopipe-azservicebus@$VERSION`
3. Report release URL

## Critical Rules

- Three mandatory approval gates (merge, tag, push) — cannot be skipped
- Must merge main back to develop after release

## Reference

@../docs/procedures/release.md
