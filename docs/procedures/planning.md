# Planning Procedures

## File Organization

Active plans live in `docs/plans/` with descriptive names. Completed plans are archived in `docs/plans/archive/` with sequential numbering (0001, 0002, ...).

## Document Template

```markdown
# Plan: Title

Status: Proposed | In Progress | Complete
Related ADRs: docs/adr/NNNN-title.md
Dependencies: none

## Overview

...

## Goals

- [ ] Goal 1
- [ ] Goal 2

## Tasks

### Task 1: Description

Files to modify: ...

Acceptance criteria:
- [ ] ...

## Implementation Order

1. Task 1
2. Task 2

## Acceptance Criteria

- [ ] All tasks complete
- [ ] Tests pass
- [ ] CHANGELOG updated
```

## Plan States

- **Proposed**: Documented but not started
- **In Progress**: Active development
- **Complete**: Done; move to `docs/plans/archive/`

## Hierarchical Dependencies

Large initiatives can use numbered plan sequences with explicit dependency declarations, allowing each plan to remain independently implementable once prerequisites are satisfied.

## Decision Documents

For significant architectural decisions within a plan, create `<plan-name>.decisions.md` with rejected alternatives and trade-offs. Status: `Resolved`, `Open`, or `Superseded`.
