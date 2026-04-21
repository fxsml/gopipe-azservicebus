# Development Procedures

Modular procedure library for gopipe-azservicebus development.

## Quick Reference

| Topic | Procedure | Summary |
|-------|-----------|---------|
| Git | [git.md](git.md) | Branch naming, commits, version management |
| Go | [go.md](go.md) | Build, test, vet, godoc, error handling |
| Documentation | [documentation.md](documentation.md) | CHANGELOG, ADRs, doc linting |
| Planning | [planning.md](planning.md) | Plan files and templates |
| ADRs | [adr.md](adr.md) | Architecture decision records lifecycle |
| Feature Release | [feature-release.md](feature-release.md) | Feature branch merge with history cleanup |
| Release | [release.md](release.md) | Tagging and GitHub release |

## Standard Practices

- Run `go test ./...` and `go build ./...` and `go vet ./...` before every commit
- Use conventional commits: `feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`
- Feature branches from develop; hotfixes from main
- All merges to main via PR through develop
