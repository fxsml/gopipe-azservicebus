# Managing Git Workflow

## Branch Naming

| Branch | Purpose | Base |
|--------|---------|------|
| `feature/*` | New features | develop |
| `release/*` | Release preparation | develop |
| `hotfix/*` | Critical fixes | main |
| `claude/*` | Claude-generated branches | develop |

## Commit Conventions

Conventional commits required: `<type>(<scope>): <description>`

Types: `feat`, `fix`, `docs`, `style`, `refactor`, `test`, `chore`

Examples:
```
feat(subscriber): add lock renewal interval override
fix(publisher): check context before retry attempt
docs: update KNOWN-ISSUES for lock expiry mitigation
test(semaphore): add race condition test
chore: upgrade azure-sdk-for-go to v1.11.0
```

## Release Tagging

Single-module — one tag per release:

```bash
git tag vX.Y.Z
git push origin vX.Y.Z
```

Tag from main only, after merging develop → main.

## Approval Gates

**Always pause and ask for explicit approval before:**
- Interactive rebase or history rewrite
- Force push to any branch
- Merge to develop or main
- Creating or pushing tags
- Creating GitHub releases

## Key Rules

- Never merge directly to main — always PRs through develop
- Never force push to main or develop
- Run `go test ./... && go build ./... && go vet ./...` before push
- Document before push: CHANGELOG, ADRs, godoc

## Common Mistakes

- Force pushing without lease (`--force` instead of `--force-with-lease`)
- Forgetting to sync develop after release (`git merge main` → develop)

## Reference Procedures

- @../docs/procedures/git.md — branch naming, commits, version management
- @../docs/procedures/feature-release.md — full merge workflow with approval gates
- @../docs/procedures/release.md — tagging and GitHub release
