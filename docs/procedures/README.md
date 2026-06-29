# Development Procedures

## Procedures Index

| File | Topic | Description |
|------|-------|-------------|
| [coding.md](coding.md) | Coding | Behavioral rules: simplicity, scope discipline, TDD |
| [git.md](git.md) | Git | Workflow, branching, commits |
| [go.md](go.md) | Go | Standards, godoc, testing |
| [release.md](release.md) | Releases | Release and hotfix procedures |

## Quick Reference

### Before Every Commit
```bash
go test ./... && go build ./... && go vet ./...
```

### Commit Message Format
```
<type>(<scope>): <description>

Types: feat, fix, docs, style, refactor, test, chore, ci
```

### Branch Naming
```
feature/<name>    # New features
hotfix/<name>     # Critical fixes
claude/<name>     # Claude-generated branches
```
