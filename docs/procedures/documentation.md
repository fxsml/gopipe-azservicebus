# Documentation Procedures

## Quick Start

```bash
# Run documentation lint (Claude-assisted)
/docs-lint
```

## Documentation Lint Procedure

### Step 1: Check Broken Internal Links

Verify all internal markdown links resolve to existing files:

```bash
grep -roh '\[.*\]([^http][^)]*\.md)' docs/ | \
  sed 's/.*(\(.*\))/\1/' | sort -u
```

Check for: links to non-existent files, moved/renamed files, incorrect relative paths.

### Step 2: Verify ADR Index

Compare ADR files with README index:

```bash
ls docs/adr/*.md | grep -v README
grep -E '^\| \[' docs/adr/README.md
```

Check for: ADRs missing from index, index entries for deleted ADRs, status mismatches.

### Step 3: Verify Procedures Index

Compare procedure files with README index:

```bash
ls docs/procedures/*.md | grep -v README
grep -E '^\| \[' docs/procedures/README.md
```

Check for: procedures missing from index, index entries for deleted procedures.

### Step 4: Check CHANGELOG

Verify CHANGELOG structure:

```bash
head -50 CHANGELOG.md
```

Check for: `[Unreleased]` section exists at top, version sections have dates, no duplicate version entries.

### Step 5: Check Plans Index

Compare plan files with README index:

```bash
ls docs/plans/*.md | grep -v README
ls docs/plans/archive/*.md 2>/dev/null
cat docs/plans/README.md
```

Check for: active plans missing from "Current" table, archived plans missing from "Archive" table, status mismatches.

### Step 6: Report

```
Documentation Lint Report
=========================
[ ] Broken links: X found
[ ] ADR index: X missing, X stale
[ ] Procedures index: X missing, X stale
[ ] CHANGELOG: OK / Issues found
[ ] Plans index: X missing, X stale
```

## Directory Structure

```
docs/
├── adr/          # Architecture Decision Records
├── plans/        # Implementation plans (active + archive/)
│   └── archive/
└── procedures/   # Development procedures (this)
```

## Before Pushing Code

- [ ] CHANGELOG.md updated under `[Unreleased]`
- [ ] ADR created for architectural decisions (see [adr.md](adr.md))
- [ ] Godoc for public APIs
- [ ] README.md updated for interface/config changes

## Maintenance

Before release:
- [ ] Move `[Unreleased]` to version section in CHANGELOG
- [ ] Update ADR statuses (Accepted → Implemented)
- [ ] Move completed plans to `docs/plans/archive/`
