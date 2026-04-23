---
description: Run backend tests, update docs, commit, and push (no release tag)
---

Run backend tests, update docs, commit, and push — without creating a release tag.

## Steps

### 1. Run backend tests

Run `cd backend && go test ./...` and verify all tests pass. If any test fails, stop and report the failure — do NOT proceed with commit or push.

### 2. Update documentation

Review all staged and unstaged changes (!`git diff` and !`git diff --cached`) and update these docs to reflect the changes:
- `docs/ROADMAP.md` — mark completed phases/features, add new ones
- `docs/DECISIONS.md` — add ADR entries for any architectural decisions made
- `AGENTS.md` — update Development Status section and any relevant architecture notes

Do NOT ask for confirmation — just update what needs updating based on the diff.

### 3. Commit

- Stage all changed files (including the doc updates)
- Write a concise commit message that focuses on the "why" (1-2 sentences)
- Do NOT add `Co-Authored-By` lines
- Commit

### 4. Push

- Push to the remote (origin) on the current branch
