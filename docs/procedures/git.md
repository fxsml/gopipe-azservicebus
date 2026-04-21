# Git Procedures

## Branch Strategy

```
main        ← production-ready, tagged releases
develop     ← integration branch
feature/*   ← new features, branch from develop
hotfix/*    ← critical fixes, branch from main
release/*   ← release preparation, branch from develop
claude/*    ← Claude-generated branches, base from develop
```

## Commit Conventions

Format: `<type>(<scope>): <description>`

Types: `feat`, `fix`, `docs`, `style`, `refactor`, `test`, `chore`

Examples:
```
feat(subscriber): add lock renewal interval override
fix(publisher): handle context cancellation before retry
docs: update known issues for lock expiry mitigation
test(subscriber): add reconnection integration test
chore: upgrade azure-sdk-for-go to v1.11.0
refactor(semaphore): simplify acquire with context
```

## Feature Workflow

1. `git checkout develop && git pull origin develop`
2. `git checkout -b feature/<name>`
3. Develop, commit with conventional messages
4. Open PR to develop
5. After merge, delete feature branch

## Version Management

Single-module — one tag per release following semver:

```bash
git tag vX.Y.Z
git push origin vX.Y.Z
```

Tag from main only, after merging develop → main.

Semver rules:
- **MAJOR**: breaking API changes (`NewSubscriber` signature change, removed types)
- **MINOR**: new features, backward-compatible
- **PATCH**: bug fixes only

## Critical Rules

- Never merge directly to main — always PR through develop
- Never force push to main or develop
- Run `go test ./... && go build ./... && go vet ./...` before push
- Document before push: CHANGELOG, ADRs, godoc
- Use `--force-with-lease` if force push to feature branch is needed (never to main/develop)
