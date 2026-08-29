# Project

## Product goal

LifeLog is a minimalist, self-hosted personal journal. It should make daily
journaling quick on a phone and keep journal data on a server or device controlled
by the user.

## Priorities and philosophy

Every trade-off follows this order:

1. Simplicity
2. Reliability
3. Speed
4. Features

LifeLog is:

- self-hosted and open source;
- mobile-first, fast, and suitable for weak hardware;
- easy to understand, operate, back up, and move;
- usable over a LAN or a private network such as Tailscale;
- multi-user capable without making a single-user setup cumbersome.

Privacy primarily comes from keeping journal data on the user's own system and
not sending it to a third party. Complex database encryption is not a project
requirement.

## Product scope

An installation contains local user profiles. A profile does not require an
email address and may optionally be protected by a PIN or password. Each user has
one journal in the MVP, while the data model allows multiple journals per user in
the future. A one-user installation with no PIN may later enter that user's
journal directly.

The main journal screen represents one date. It is always editable: there is no
separate read mode. Changes happen locally in the browser, controls do not save
individually, and one Save action writes the complete day atomically in a single
SQLite transaction. Users can move to the previous day, next day, today, or a
date selected with a date picker.

Journal questions are data, not application features. Users can create, rename,
reorder, deactivate, and reactivate questions. Initial types are short text, long
text, yes/no, number, scales from 1–5 and 1–10, time, select, and multi-select.
Historical answers must remain understandable when questions or options change.

## MVP scope

- Local profiles, sessions, and optional PIN/password protection
- One journal per user in the interface
- Custom questions and options
- A daily entry with general note, special moment, location, and custom answers
- One atomic Save operation for a complete day
- Optional filesystem-backed photos
- Rebuildable SQLite full-text search across journal text
- Mobile-first responsive UI and installable PWA behavior
- Docker deployment on Linux AMD64 and ARM64
- Real-world testing and reliability polish

## Non-goals

The MVP does not include:

- hosted accounts, email registration or verification, OAuth, or CAPTCHA;
- complex roles or permissions, JWT authentication, or database encryption;
- React, Vue, Svelte, or a Node-based frontend build pipeline;
- PostgreSQL, Redis, an ORM, GraphQL, background workers, or microservices;
- offline editing or synchronization;
- AI, semantic search, or natural-language journal queries;
- preset modules, exports, a backup UI, calendar overview, multiple journals in
  the interface, voice notes, or general attachments.

## Technical direction

LifeLog is one Go application running as one process. It uses
`modernc.org/sqlite`, server-rendered `html/template` pages, vanilla JavaScript,
and simple CSS. Frontend assets may be embedded in the Go binary. SQLite is the
source of truth; photos live in a data directory and are referenced by relative
path. Search uses a rebuildable SQLite FTS index.

Dependencies and infrastructure must stay minimal. The application must remain
within the practical resource limits of a Raspberry Pi Zero 2 W with 512 MB RAM.

## Target platforms and deployment

The initial environment is an Ubuntu 24.04 always-on home server using Docker,
Docker Compose, and optionally Tailscale. Linux AMD64 and ARM64 are first-class
targets. Raspberry Pi OS Lite on a Raspberry Pi Zero 2 W is a later deployment
target.

Persistent state should be portable between these machines and live under a
single data directory:

```text
data/
├── journal.db
├── photos/
└── config/
```

A durable backup is fundamentally the SQLite database plus the photos directory
and any required local configuration.

## Long-term principles

- Preserve relational journal data as the source of truth.
- Preserve the meaning of historical answers as configuration evolves.
- Prefer deactivation over destructive deletion when history refers to a record.
- Keep derived data, including search indexes, fully rebuildable.
- Make data and backups portable across supported architectures.
- Treat presets as templates that create ordinary questions, not backend modules.
- Add features only when they do not compromise the project's ordered priorities.
