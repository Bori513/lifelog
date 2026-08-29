# Agent instructions

Before implementing a task, read the relevant files in `docs/`:

- `docs/PROJECT.md` is the source of truth for product scope, philosophy, and
  technical direction.
- `docs/DATABASE.md` is the source of truth for the current database design.
- `docs/DECISIONS.md` records approved architectural decisions and their reasoning.
- `docs/ROADMAP.md` defines implementation phases and current progress.

## Working rules

- Prefer, in this exact order: simplicity > reliability > speed > features.
- Do not silently change approved architecture or database design. If a change
  appears necessary, explain it and wait for approval before making the
  architectural change.
- Avoid adding dependencies unless they provide a clear benefit and fit the
  project's minimalist philosophy.
- Keep Linux ARM64 and Raspberry Pi Zero 2 W constraints in mind when making
  implementation choices.
- Do not introduce technologies explicitly excluded by `docs/PROJECT.md`.
- Keep code, comments, documentation, commit messages, and user-facing development
  artifacts in English.
- Keep changes scoped to the requested phase or task.
- Update project documentation when an approved implementation decision makes it
  outdated.
- After code changes, run the relevant formatting, tests, build checks, and
  `git diff --check`.
- Do not commit or push unless explicitly asked.
