# Architectural decisions

These decisions define the current direction. Revisit them only when new evidence
outweighs the project's priority order: simplicity, reliability, speed, features.

## Application and platform

- **Use a Go monolith running as one process.** A single application is easier to
  understand, deploy, and operate on weak hardware.
- **Support Linux AMD64 and ARM64 as first-class targets.** The same project must
  run on a home server and resource-constrained Raspberry Pi hardware.
- **Deploy with Docker and Docker Compose initially.** This provides a small,
  repeatable self-hosted deployment without adding an orchestration platform.
- **Keep dependencies and infrastructure minimal.** Every dependency consumes
  maintenance effort and can work against portability and low resource use.

## Data and storage

- **Use SQLite through `modernc.org/sqlite`.** SQLite keeps persistent data
  portable and operations simple; the selected driver avoids a required C toolchain.
- **Do not use an ORM.** Direct SQL keeps behavior visible and avoids a large
  abstraction in a deliberately small application.
- **Save a complete day in one request and one SQLite transaction.** Local UI
  changes remain instant while the journal day is committed atomically.
- **Store typed answers relationally rather than in a primary JSON blob.** Typed
  columns and option relations support validation, querying, and durable history.
- **Store photos on the filesystem.** SQLite contains metadata and a relative path,
  keeping the database small and the backup layout understandable.
- **Upload photos only with the manual whole-day Save.** The browser previews
  selected files locally; the authenticated multipart day request validates JPEG,
  PNG, or WebP content on the server before staged files become permanent.
- **Serve uploaded photos through authenticated routes.** The application checks
  ownership through photo, day, journal, and user records instead of exposing the
  photo directory as static content.
- **Treat FTS as rebuildable derived data.** Normal relational journal records are
  the source of truth, so loss or corruption of the index is recoverable.
- **Index one derived FTS5 document per journal day.** Application code constructs
  each document from authoritative relational data and historical label snapshots,
  updates it atomically with a day Save, and can rebuild the full index. Search is
  scoped in SQL to one journal and orders matches by newest entry date first.
- **Create whole-instance backups from a SQLite snapshot plus filesystem photos.**
  Backup creation uses `VACUUM INTO`, reads photo references from that snapshot,
  streams the complete photos tree into a temporary ZIP, and succeeds only when
  every referenced photo was included. One process-wide non-blocking guard limits
  backup creation to one operation. Optional server backups publish with a final
  same-filesystem rename only after the archive is complete.

## Web interface

- **Render HTML on the server with `html/template`.** This supports a fast,
  understandable interface with little runtime or tooling overhead.
- **Use the standard library HTTP server and router.** `net/http` provides the
  small route set and server safety controls needed by the application without
  adding a framework dependency.
- **Relax the HTTP write deadline only for completed backup downloads.** Normal
  responses retain the global write timeout. Once a backup ZIP is fully created,
  its handler clears that response's write deadline through `http.ResponseController`
  so a slow link does not truncate the download.
- **Embed templates and static assets in the application binary.** The web UI
  has no separate runtime asset deployment or frontend build step.
- **Use vanilla JavaScript and simple CSS.** The interface does not require a
  frontend framework or Node-based build pipeline.
- **Use database-backed bearer sessions in secure cookies.** The browser stores
  only the plaintext bearer token in an `HttpOnly`, `SameSite=Lax` cookie; the
  database retains only its hash. Cookie transport security is configurable for
  local HTTP or HTTPS deployment.
- **Protect state-changing forms with CSRF tokens.** A cryptographically random
  double-submit cookie token covers both pre-session authentication forms and
  authenticated journal forms without adding a framework.
- **Use Post/Redirect/Get after a successful whole-day Save.** The daily form is
  committed in one request through the existing atomic domain operation, then
  redirects to prevent accidental resubmission and display Save status.
- **Determine `/today` in the authenticated profile's timezone.** Journal dates
  represent the user's local calendar day rather than the server's UTC date.
- **Build mobile-first and provide PWA capabilities.** Daily use is phone-centered;
  installation and standalone display are useful without requiring offline sync.
- **Do not implement offline journal sync in the MVP.** Synchronization would add
  substantial state and conflict complexity to an otherwise server-owned journal.
- **Keep the PWA service worker limited to static presentation assets.** It uses a
  versioned cache for CSS, JavaScript, icons, and a private-data-free offline page.
  Authenticated pages, photos, and application data remain network-dependent and
  are never intentionally cached; there is no second client-side journal database.
- **Require a secure context for full PWA behavior outside local development.**
  Browsers allow service workers on localhost, but phone installation and service
  workers generally require HTTPS. Deployment may provide it through an
  HTTPS-capable Tailscale setup or reverse proxy rather than TLS in the Go app.

## Product model

- **Make questions data-defined.** Custom questions and future presets use the same
  generic model, avoiding feature-specific backend modules.
- **Do not change a question's type after answers exist.** Deactivate the old
  question and create a new one so historical values retain their meaning.
- **Deactivate historically referenced questions and options instead of normally
  deleting them.** Renames affect the current configuration, while
  `question_label_snapshot` and `option_label_snapshot` retain the original
  question and selected-option wording in historical answers.
- **Support multiple users while optimizing for one.** Local profiles allow shared
  installations, while optional PINs and a possible direct-entry path keep personal
  installations simple.
- **Store optional PIN/password credentials as bcrypt hashes.** Credentials are
  one-way hashed with bcrypt and are never stored or exposed as plaintext.
- **Use hashed, high-entropy bearer tokens for sessions.** Session tokens contain
  256 bits of randomness from the operating system. Only their deterministic
  SHA-256 hashes are stored, which supports lookup without retaining bearer tokens.
- **Create the initial journal with its profile atomically.** Every new profile
  receives a `Personal` journal in the same transaction, preventing partial setup.
- **Derive single-user direct-entry eligibility.** Exactly one profile with no PIN
  is eligible; this is calculated from profile data rather than stored as a separate
  application mode.
- **Model journals separately from users.** The MVP exposes one journal per user,
  but the schema can support more without a migration of historical entries.
- **Treat presets as question templates, not subsystems.** Applying a preset simply
  creates ordinary questions that the user can then customize.
- **Do not include AI in the MVP.** Conventional SQLite FTS meets initial search
  needs without privacy, resource, or product complexity.
