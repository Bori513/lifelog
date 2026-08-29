# LifeLog

LifeLog is a minimalist, self-hosted personal journal. It is designed to keep a
person's journal on hardware they control while remaining easy to understand,
back up, move, and run on modest devices.

> LifeLog is in early development. Back up your data and expect changes before a
> stable release.

## Priorities

LifeLog makes trade-offs in this order:

1. Simplicity
2. Reliability
3. Speed
4. Features

The project is mobile-first, open source, multi-user capable, and friendly to a
single-user installation. It avoids unnecessary services and frontend tooling.

## Stack

- Go monolith
- SQLite through `modernc.org/sqlite`
- Server-rendered HTML with `html/template`
- Vanilla JavaScript and simple CSS
- Progressive Web App support
- Photos on the filesystem, with metadata in SQLite
- Docker deployment for Linux AMD64 and ARM64

## Docker quick start

Clone the repository and start the one-container deployment:

```bash
git clone https://github.com/Bori513/lifelog.git
cd lifelog
docker compose up -d
```

Open `http://SERVER_IP:8080`. The first page guides you through creating the
first local profile. Follow logs with `docker compose logs -f` and stop the
service with `docker compose down`.

Compose bind-mounts `./data` at `/data`; do not remove that directory. The image
runs as root inside the container so a newly created bind mount is writable on a
wide range of Docker hosts without UID/GID setup. The container has no privileged
mode, host filesystem mount, or bundled sidecar service.

The default Compose setting uses `LIFELOG_SECURE_COOKIES=false` for plain local
HTTP. When LifeLog is served through trusted HTTPS, set it to `true` and recreate
the container. Docker does not provide HTTPS. Tailscale Serve, Caddy, nginx, or
another trusted reverse proxy can provide HTTPS without becoming a LifeLog
dependency. A host already connected to Tailscale can also expose port 8080 over
that private network; no Tailscale software runs in this stack.

LifeLog works as a normal web app over LAN or Tailscale HTTP. Service workers and
PWA installation generally require HTTPS on a real phone; `localhost` is the
browser development exception. Do not expect full PWA installation from an
arbitrary plain-HTTP IP address.

The unauthenticated `GET /healthz` endpoint returns only database availability and
no journal data.

## Data, backups, and moving hosts

The persistent state is `data/journal.db` (including SQLite WAL/SHM sidecars while
running) and `data/photos/`. Stop the container with `docker compose down` before
a simple filesystem backup; copying only `journal.db` while LifeLog is writing in
WAL mode is not a safe backup procedure.

To move between AMD64 and ARM64 machines, stop LifeLog, copy the complete `data/`
directory, and start LifeLog on the destination. SQLite needs no architecture
conversion.

## MVP direction

The MVP will provide local profiles, configurable journal questions, an editable
daily entry saved atomically in one operation, optional photos, SQLite full-text
search, and a mobile-first PWA experience. Offline synchronization and AI are
not part of the MVP.

Project scope and design are documented in [`docs/PROJECT.md`](docs/PROJECT.md),
[`docs/ROADMAP.md`](docs/ROADMAP.md), [`docs/DECISIONS.md`](docs/DECISIONS.md),
and [`docs/DATABASE.md`](docs/DATABASE.md).

## PWA access

LifeLog can be installed from a supported browser and launched in standalone
mode. The installation layer caches only static presentation assets; journal
pages, photos, and writes always require a connection to the LifeLog server.

Browsers treat `localhost` as a secure context for development. A phone may open
LifeLog from a LAN address such as `http://192.168.x.x:8080`, but service-worker
and installation behavior is generally restricted on that insecure origin. A
real phone installation should use HTTPS, provided in a future deployment by an
HTTPS-capable Tailscale setup or reverse proxy. LifeLog does not terminate TLS or
configure Tailscale itself.
