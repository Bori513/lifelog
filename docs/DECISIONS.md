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
- **Treat FTS as rebuildable derived data.** Normal relational journal records are
  the source of truth, so loss or corruption of the index is recoverable.

## Web interface

- **Render HTML on the server with `html/template`.** This supports a fast,
  understandable interface with little runtime or tooling overhead.
- **Use vanilla JavaScript and simple CSS.** The interface does not require a
  frontend framework or Node-based build pipeline.
- **Build mobile-first and provide PWA capabilities.** Daily use is phone-centered;
  installation and standalone display are useful without requiring offline sync.
- **Do not implement offline journal sync in the MVP.** Synchronization would add
  substantial state and conflict complexity to an otherwise server-owned journal.

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
- **Model journals separately from users.** The MVP exposes one journal per user,
  but the schema can support more without a migration of historical entries.
- **Treat presets as question templates, not subsystems.** Applying a preset simply
  creates ordinary questions that the user can then customize.
- **Do not include AI in the MVP.** Conventional SQLite FTS meets initial search
  needs without privacy, resource, or product complexity.
