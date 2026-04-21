# ADR Procedures

Architecture Decision Records document significant technical choices.

## When to Create an ADR

**Create for:**
- Changing public API signatures
- Adding/removing interfaces or types
- Changing architectural patterns (e.g., reconnection strategy, semaphore design, shutdown pattern)

**Skip for:**
- Bug fixes
- Internal refactoring with no API impact
- Documentation updates

## Lifecycle

```
Proposed → Accepted → Implemented
                    ↘ Superseded (by NNNN)
```

## File Organization

`docs/adr/NNNN-short-title.md` — sequential numbering, lowercase hyphenated.

## Template

```markdown
# NNNN. Short Title

Date: YYYY-MM-DD
Status: Proposed

## Context

Why this decision is needed.

## Decision

What was decided. Include code examples where helpful.

## Consequences

Breaking changes, benefits, drawbacks. Cross-reference related or superseded ADRs.
```

## Updating an ADR

Document status changes in both the header and README index. If superseding, mark old ADR with replacement number and reference predecessor in new one. Add updates in an "Updates" section rather than editing in place.

## Quality Checks Before Merging

- Sequential numbering
- Status accurate (Proposed/Accepted/Implemented/Superseded)
- `docs/adr/README.md` index updated
- Cross-references correct
- Code examples match implementation
