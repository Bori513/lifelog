# LifeLog

LifeLog is a simple, self-hosted daily journal built around questions you define
yourself.

> LifeLog is an early project under active development. Back up your data and
> expect changes before a stable release.

## Why I built it

LifeLog started as a personal project. I often forget ordinary details from my
days: what I did, where I went, whether I worked out, or small moments I would
like to remember later.

I wanted a private place where I could spend a minute or two answering questions
I chose myself, gradually building a searchable history on my own server instead
of depending on a hosted journaling service.

## Features

- A daily journal with a general note, memorable moment, location, and photos.
- Custom, reorderable questions: short text, long text, Workout, yes/no, number,
  1–5 and 1–10 scales, time, select, and multi-select.
- Workout questions with compact raw-text gym notation, a live preview, and a
  focused editor. Questions can also be pinned to the top of the daily form in
  the current browser.
- Question and option deactivation that preserves understandable historical
  answers when the journal configuration changes.
- Full-text search across journal text and answers.
- Browse / Day Overview with date ranges and type-aware question filters.
- A monthly Calendar that reuses the question filters and can show optional
  markers configured per question.
- Multiple local profiles, with optional PIN or password protection. The current
  interface provides one journal per profile.
- Mobile-first responsive interface with bottom navigation and installable PWA
  behavior. Journal data remains online-only and is not cached for offline
  editing.
- System, light, and dark appearance modes with eight browser-local themes.
- Whole-instance ZIP backup downloads, optional server-side backup storage, and
  a documented offline restore workflow.

## Philosophy and design

LifeLog makes trade-offs in this order: **simplicity > reliability > speed >
features**. It deliberately uses a small stack and avoids services that are not
needed for a dependable personal journal.

## Quick start

Docker and Docker Compose are the intended deployment path:

```bash
git clone https://github.com/Bori513/lifelog.git
cd lifelog
docker compose up -d
```

Open `http://SERVER_IP:8080` and create the first local profile. View logs with
`docker compose logs -f` and stop LifeLog with `docker compose down`.

Compose stores persistent state in `./data` on the host, mounted at `/data` in
the container. Keep this directory when recreating or updating the container.

The default Compose configuration uses plain HTTP and sets secure cookies off.
For access beyond a trusted local or private network, put LifeLog behind HTTPS
and set `LIFELOG_SECURE_COOKIES=true`. Docker does not provide HTTPS; a reverse
proxy or an HTTPS-capable Tailscale setup can provide it.

## Updating

Create a backup first, then update the checkout and rebuild the container:

```bash
git pull --ff-only
docker compose up -d --build
```

The persistent `./data` directory is not replaced by this process. Check startup
afterward with `docker compose logs` or `http://SERVER_IP:8080/healthz`.

## Backup and restore

Open **Settings → Backup** to create and download a ZIP of the entire LifeLog
instance. It contains a standalone SQLite snapshot, the complete `photos/` tree,
and a small backup manifest. The backup covers every local profile.

To restore, stop LifeLog, preserve the current `data/` directory somewhere safe,
and extract the backup. Copy `lifelog-backup/journal.db` and its `photos/`
directory into the persistent data directory, then start LifeLog and verify your
entries and photos. Never restore into a running instance.

LifeLog can also write backups to a server directory when
`LIFELOG_BACKUP_DIR` is configured and that directory is mounted into the
container. For protection against disk failure, store those backups on a
different device from the primary data.

To create a server backup without using the web interface, run:

```bash
docker compose exec -T lifelog lifelog backup
```

The command uses `LIFELOG_DATA_DIR` and `LIFELOG_BACKUP_DIR`, prints the created
ZIP filename, and exits with a non-zero status if the backup fails. It is suitable
for invocation by a systemd timer or another scheduler.

## Architecture

LifeLog is one Go application running as one process and normally deployed as a
single container with persistent data. It uses SQLite, server-rendered
`html/template` pages, vanilla JavaScript and CSS, filesystem-backed photos, and
a small PWA layer. The image builds for Linux AMD64 and ARM64.

There is no Node frontend runtime or build pipeline, and no PostgreSQL, Redis,
background worker, or companion service.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `LIFELOG_DATA_DIR` | `./data` (`/data` in Docker) | SQLite database and photo storage |
| `LIFELOG_ADDR` | `:8080` | HTTP listen address |
| `LIFELOG_SECURE_COOKIES` | `false` | Enable the `Secure` flag on session and CSRF cookies; use with HTTPS |
| `LIFELOG_BACKUP_DIR` | empty | Existing directory for optional server-side backups |

The included `compose.yaml` maps host port `8080`, mounts `./data:/data`, and
uses `LIFELOG_BACKUP_DIR` from the host environment when set.

## PWA and mobile access

LifeLog works as a normal web app over a LAN or private network. Installation as
a PWA and service-worker support generally require HTTPS on a phone; `localhost`
is the browser development exception. The service worker caches presentation
assets and an offline page only. Entries, photos, and writes always require a
connection to the LifeLog server.

## Data and privacy

Journal records and photo metadata live in `data/journal.db`; uploaded photos
live under `data/photos/`. Photos are served through authenticated application
routes rather than exposing the directory directly. LifeLog has no hosted
account requirement: the person operating the server controls where the
application and its data run.

Optional PINs and passwords are stored as bcrypt hashes, but self-hosting does
not replace normal server, network, access-control, and backup security. Journal
contents are not encrypted at rest by the application.

## Project status

LifeLog was originally built for personal use and is still an early project.
Most planned MVP functionality is implemented; real-world testing and reliability
polish are ongoing. Feedback from other self-hosters is welcome, but the project
should not yet be treated as production-certified or professionally audited.

Project scope and decisions are documented in
[`docs/PROJECT.md`](docs/PROJECT.md), [`docs/ROADMAP.md`](docs/ROADMAP.md),
[`docs/DECISIONS.md`](docs/DECISIONS.md), and
[`docs/DATABASE.md`](docs/DATABASE.md).

## Feedback and contributing

Bug reports, UX feedback, feature ideas, and pull requests are welcome through
[GitHub Issues](https://github.com/Bori513/lifelog/issues). Please keep proposals
consistent with the project's minimalist scope and priorities.

## License

LifeLog is licensed under the MIT License. See [LICENSE](LICENSE).
