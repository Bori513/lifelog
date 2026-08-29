# LifeLog

LifeLog is a minimalist, self-hosted personal journal. It is designed to keep a
person's journal on hardware they control while remaining easy to understand,
back up, move, and run on modest devices.

> LifeLog is in early development. The database foundation is implemented, but
> the application is not ready for journal use.

## Priorities

LifeLog makes trade-offs in this order:

1. Simplicity
2. Reliability
3. Speed
4. Features

The project is mobile-first, open source, multi-user capable, and friendly to a
single-user installation. It avoids unnecessary services and frontend tooling.

## Planned stack

- Go monolith
- SQLite through `modernc.org/sqlite`
- Server-rendered HTML with `html/template`
- Vanilla JavaScript and simple CSS
- Progressive Web App support
- Photos on the filesystem, with metadata in SQLite
- Docker deployment for Linux AMD64 and ARM64

## MVP direction

The MVP will provide local profiles, configurable journal questions, an editable
daily entry saved atomically in one operation, optional photos, SQLite full-text
search, and a mobile-first PWA experience. Offline synchronization and AI are
not part of the MVP.

Project scope and design are documented in [`docs/PROJECT.md`](docs/PROJECT.md),
[`docs/ROADMAP.md`](docs/ROADMAP.md), [`docs/DECISIONS.md`](docs/DECISIONS.md),
and [`docs/DATABASE.md`](docs/DATABASE.md).
