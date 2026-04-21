# Docs Lint

Check documentation quality and consistency.

## Steps

1. **Check broken internal links** — verify all markdown links in `docs/` resolve to existing files
2. **Verify ADR index** — cross-reference `docs/adr/*.md` with `docs/adr/README.md`
3. **Verify procedures index** — cross-reference `docs/procedures/*.md` with `docs/procedures/README.md`
4. **Check CHANGELOG** — confirm `[Unreleased]` section exists, versions have dates, no duplicates
5. **Check plans index** — verify active/archived plans match `docs/plans/README.md`

## Output

```
Documentation Lint Report
=========================
[ ] Broken links: X found
[ ] ADR index: X missing, X stale
[ ] Procedures index: X missing, X stale
[ ] CHANGELOG: OK / Issues found
[ ] Plans index: X missing, X stale
```

## Available Tools

Bash, Read, Glob, Grep

## Reference

@../docs/procedures/documentation.md
