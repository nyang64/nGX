# Project Instructions for AI Agents

This file provides instructions and context for AI coding agents working on this project.

<!-- BEGIN BEADS INTEGRATION v:1 profile:minimal hash:ca08a54f -->
## Beads Issue Tracker

This project uses **bd (beads)** for issue tracking. Run `bd prime` to see full workflow context and commands.

### Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --claim  # Claim work
bd close <id>         # Complete work
```

### Rules

- Use `bd` for ALL task tracking — do NOT use TodoWrite, TaskCreate, or markdown TODO lists
- Run `bd prime` for detailed command reference and session close protocol
- Use `bd remember` for persistent knowledge — do NOT use MEMORY.md files

## Working Branch

**All work happens on `aws-serverless-stack`.** Never commit directly to `main`.

```bash
git checkout aws-serverless-stack   # ensure you're on the right branch
git pull --rebase                   # sync before starting work
```

## Session Completion

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   bd dolt push
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
<!-- END BEADS INTEGRATION -->


## Build & Test

**MANDATORY RULE: Always run tests before `git commit` and `git push`. No exceptions.**

```bash
# Unit tests (run once — no loadenv needed; DATABASE_URL is unset by the target)
make test

# Integration tests (run TWICE — catches flakiness; requires source loadenv.sh first)
source loadenv.sh && make test-integration
source loadenv.sh && make test-integration

# Then commit and push
git add <files>
git commit -m "..."
git pull --rebase && git push
```

Integration tests require `TEST_BASE_URL`, `TEST_API_KEY`, `TEST_LAMBDA_PREFIX`, and
`TEST_AWS_REGION` — all sourced from `.env.outputs` via `loadenv.sh`.

Unit tests must NOT have `DATABASE_URL` set — `ses_events/init()` skips DB init when
it's empty, which is correct for unit tests (tests set `pool = nil` themselves). The
`make test` target enforces this with `DATABASE_URL=`.

## Architecture Overview

_Add a brief overview of your project architecture_

## Conventions & Patterns

_Add your project-specific conventions here_
