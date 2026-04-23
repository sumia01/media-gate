---
description: Run backend tests, update docs, commit, push, and create a release tag
---

Run backend tests, complete the current work by updating docs, committing, pushing, and creating a release tag.

## Steps

### 1. Run backend tests

Run `cd backend && go test ./...` and verify all tests pass. If any test fails, stop and report the failure — do NOT proceed with commit, push, or tagging.

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

### 5. Release tag

Determine the version bump automatically based on the commit content:

- **minor** bump: new user-facing features, new UI pages/sections, new API endpoints, new backend services
- **patch** bump: bug fixes, styling tweaks, refactors, doc-only changes, dependency updates, small UI adjustments

Look at the latest tag with !`git tag --sort=-v:refname | head -1`, bump accordingly, create the tag, and push it. No need to ask the user — just do it.
