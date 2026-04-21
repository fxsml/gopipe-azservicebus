#!/usr/bin/env bash
# PreToolUse hook: run quality checks before git commit/push

TMPFILE=$(mktemp)
LOGFILE=$(mktemp)
trap "rm -f $TMPFILE $LOGFILE" EXIT
cat > "$TMPFILE"

CMD=$(python3 -c "
import sys, json
with open(sys.argv[1]) as f:
    data = json.load(f)
print(data.get('tool_input', {}).get('command', ''))
" "$TMPFILE" 2>/dev/null || echo "")

if [[ "$CMD" != *"git commit"* && "$CMD" != *"git push"* ]]; then
    exit 0
fi

cd "$CLAUDE_PROJECT_DIR" || exit 0

if ! go test ./... > "$LOGFILE" 2>&1; then
    echo "go test failed. Fix failing tests before committing." >&2
    cat "$LOGFILE" >&2
    exit 2
fi

if ! go build ./... > "$LOGFILE" 2>&1; then
    echo "go build failed." >&2
    cat "$LOGFILE" >&2
    exit 2
fi

if ! go vet ./... > "$LOGFILE" 2>&1; then
    echo "go vet failed." >&2
    cat "$LOGFILE" >&2
    exit 2
fi

exit 0
