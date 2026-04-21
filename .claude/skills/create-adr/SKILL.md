# Create ADR

Create a new Architecture Decision Record.

## Steps

1. Read `docs/adr/README.md` to get the next sequential number
2. Create `docs/adr/NNNN-<slugified-title>.md` with template:
   - Date: today
   - Status: Proposed
   - Context, Decision (with code examples), Consequences sections
3. Update `docs/adr/README.md` index with the new entry
4. Report the created file path

## Template

```markdown
# NNNN. Title

Date: YYYY-MM-DD
Status: Proposed

## Context

...

## Decision

...

## Consequences

...
```

## Available Tools

Bash, Read, Write, Edit, Glob

## Reference

@../docs/procedures/adr.md
