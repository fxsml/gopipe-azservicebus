# Changelog Entry

Add an entry to `CHANGELOG.md` under the `[Unreleased]` section.

## Steps

1. Read `CHANGELOG.md` to locate the `[Unreleased]` section
2. Parse `$ARGUMENTS`: first word is the subsection type (`Added`, `Changed`, `Fixed`, `Removed`, `Deprecated`)
3. Insert a bullet point in the correct subsection (create header if needed)
4. Maintain alphabetical subsection order: Added → Changed → Deprecated → Fixed → Removed
5. Report the line added

## Validation

If `$ARGUMENTS` doesn't start with a recognized subsection type, ask for clarification.

## Available Tools

Read, Edit
