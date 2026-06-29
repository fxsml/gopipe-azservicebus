# Git Procedures

## Workflow

Uses [git flow](https://www.atlassian.com/git/tutorials/comparing-workflows/gitflow-workflow).

### Branch Naming

| Branch | Purpose | Base |
|--------|---------|------|
| `main` | Production-ready | - |
| `develop` | Integration | - |
| `feature/*` | New features | develop |
| `release/*` | Release prep | develop |
| `hotfix/*` | Critical fixes | main |

## Commit Messages

Follow [conventional commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <description>

[optional body]

[optional footer]
```

**Types:** `feat`, `fix`, `docs`, `style`, `refactor`, `test`, `chore`

**Examples:**
```
feat(message): add CloudEvents validation
fix(router): handle nil handler gracefully
docs: update architecture roadmap
```

When a commit closes a GitHub issue, add `Closes #NNN` in the footer — not the subject:

```
feat(message/http): add ErrorHandler to SubscriberConfig

Longer description.

Closes #140
```

The `(#NNN)` parenthetical on GitHub is the PR number, added automatically on squash-merge.
Keep the subject clean; `Closes` in the footer is what GitHub parses to close the issue.

## Version Management

Uses [semantic versioning](https://semver.org/). Check the latest tag:

```bash
git tag --sort=-version:refname | head -5
```

## Feature Integration

### Step 1: Create Feature Branch

```bash
git checkout develop
git pull origin develop
git checkout -b feature/<name>
```

### Step 2: Create PR

```bash
gh pr create --base develop --title "feat: <name>" --body "..."
```

### Step 3: After Merge

```bash
git checkout develop && git pull origin develop
```

## Release Process

See [release.md](release.md) for full release procedures including hotfix releases and GitHub release creation.

## Rules

- Never merge to main directly — always PRs through develop
- Never force push to main or develop
- Always run `go test ./... && go build ./... && go vet ./...` before push
