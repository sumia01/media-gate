# Ship

Complete the current work by updating docs, committing, pushing, and creating a release tag.

## Steps

### 1. Update documentation

Review all staged and unstaged changes (git diff + git diff --cached) and update these docs to reflect the changes:
- `docs/ROADMAP.md` — mark completed phases/features, add new ones
- `docs/DECISIONS.md` — add ADR entries for any architectural decisions made
- `CLAUDE.md` — update Development Status section and any relevant architecture notes

Do NOT ask for confirmation — just update what needs updating based on the diff.

### 2. Commit

- Stage all changed files (including the doc updates)
- Write a concise commit message that focuses on the "why" (1-2 sentences)
- Do NOT add `Co-Authored-By` lines
- Commit

### 3. Push

- Push to the remote (origin) on the current branch

### 4. Release tag

Determine the version bump automatically based on the commit content:

- **minor** bump: new user-facing features, new UI pages/sections, new API endpoints, new backend services
- **patch** bump: bug fixes, styling tweaks, refactors, doc-only changes, dependency updates, small UI adjustments

Look at the latest tag with `git tag --sort=-v:refname | head -1`, bump accordingly, create the tag, and push it. No need to ask the user — just do it.
