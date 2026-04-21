# Review PR

Review PR `$ARGUMENTS` (or the current branch PR if no number given) against gopipe-azservicebus standards.

## Steps

1. Fetch PR details:
   ```bash
   gh pr view $ARGUMENTS
   gh pr diff $ARGUMENTS
   gh pr checks $ARGUMENTS
   ```

2. Review against each checklist item and report pass/fail with specific line references

## Review Checklist

### Code Quality
- [ ] Errors returned, not panicked (except `Must*` functions)
- [ ] Errors wrapped with context: `fmt.Errorf("op: %w", err)`
- [ ] Azure SB settlement errors checked with `sbErr.Code` (not just `err != nil`)
- [ ] Settlement errors `CodeLockLost`/`CodeClosed`/`CodeConnectionLost` → warn, not error
- [ ] Semaphore acquired before sending to channel, released in both ack and nack
- [ ] Shutdown uses `done` channel + `sync.Once` pattern (not bare bool)

### Tests
- [ ] Integration tests call `skipIfNoServiceBus(t)` or `skipIfNoManagement(t)`
- [ ] Table-driven tests for multiple cases where applicable
- [ ] Both success and error paths tested
- [ ] New public APIs have tests
- [ ] Concurrency-sensitive code tested with `-race`

### Documentation
- [ ] Public APIs have godoc (first sentence starts with function name)
- [ ] CHANGELOG.md updated under `[Unreleased]`
- [ ] ADR created for architectural decisions (API changes, new interfaces)
- [ ] No breaking changes without ADR
- [ ] KNOWN-ISSUES.md updated if new edge cases discovered

### Git
- [ ] Conventional commit messages (`feat:`, `fix:`, `docs:`, etc.)
- [ ] No merge commits in feature branch history
- [ ] PR targets `develop`, not `main`

## Output Format

```
PR #N Review: <title>

PASS ✓ / FAIL ✗ / N/A for each checklist item

Issues found:
- <specific issue with file:line reference>

Overall: APPROVED / CHANGES REQUESTED
```
