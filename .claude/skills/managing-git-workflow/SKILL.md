---
name: managing-git-workflow
description: |
  Provides expertise in git flow procedures for the gopipe-azservicebus repository.
  Apply when working with git operations, branch management, releases, or tagging.
  Covers branch naming, commit conventions, and approval gates.
user-invocable: false
---

# Managing Git Workflow

## Branch Naming

| Branch | Purpose | Base |
|--------|---------|------|
| `feature/*` | New features | develop |
| `hotfix/*` | Critical fixes | main |
| `claude/*` | Claude-generated branches | develop |

## Commit Conventions

Conventional commits required: `<type>(<scope>): <description>`

Types: `feat`, `fix`, `docs`, `style`, `refactor`, `test`, `chore`, `ci`

The type reflects the **content** of the change, not who made it.

| Content | Type | Examples |
|---------|------|---------|
| New user-facing feature | `feat` | new API, new CLI flag |
| Bug fix | `fix` | correct wrong behavior |
| User-facing docs | `docs` | CHANGELOG, godoc, README |
| Internal tooling & config | `chore` | `.claude/` skills, hooks, CLAUDE.md |
| CI/CD pipeline changes | `ci` | GitHub Actions workflows |
| Code restructure, no behavior change | `refactor` | — |
| Tests only | `test` | — |

Examples:
```
feat(subscriber): add peek mode for non-destructive reads
fix(publisher): handle nil context on shutdown
docs: update README with CLI usage
chore: add Claude Code skills and procedures
ci: add go vet to CI workflow
```

When a commit closes a GitHub issue, add `Closes #NNN` in the footer — not the subject:

```
feat(subscriber): add configurable prefetch count

Longer description of what was done and why.

Closes #42
```

The `Closes` keyword in the footer is what GitHub parses to auto-close the issue on merge.
The `(#NNN)` parenthetical you see on GitHub is the **PR number**, added automatically on
squash-merge — putting an issue number there manually conflates the two.

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
- Update CHANGELOG.md under `[Unreleased]` before push

## Common Mistakes

- Forgetting to sync develop after release (`git merge main` → develop)
- Force pushing without lease (`--force` instead of `--force-with-lease`)
- Targeting `main` instead of `develop` in a feature PR

## Reference Procedures

- @../docs/procedures/git.md — branch naming, commits, version management
- @../docs/procedures/release.md — tagging, hotfixes, GitHub release
