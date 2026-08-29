# Database design

This document describes the implemented relational model. The authoritative SQL
definition is `migrations/001_initial.sql`. SQLite is the durable source of truth
except for photo files, whose metadata and relative paths are stored in SQLite.

## Relationships

```text
users 1 ── * journals 1 ── * days 1 ── * answers
                    │           └────── * photos
                    └── * questions 1 ── * question_options
                                      └── * answers
answers * ── * question_options (through answer_options)
```

## Core tables

### `users`

| Field | Notes |
| --- | --- |
| `id` | Primary key |
| `name` | Local profile display name |
| `pin_hash` | Nullable password/PIN hash; never store the credential itself |
| `timezone` | User timezone for date-related behavior |
| `created_at` | Creation timestamp |
| `updated_at` | Last update timestamp |

Users are local profiles and require no email address.
PIN/password protection is optional. When configured, `pin_hash` contains a bcrypt
hash and is never exposed as profile data.

### `journals`

| Field | Notes |
| --- | --- |
| `id` | Primary key |
| `user_id` | Foreign key to `users.id` |
| `name` | Journal name |
| `created_at` | Creation timestamp |
| `updated_at` | Last update timestamp |

One user owns many journals at the database level. The MVP interface exposes one.
Creating a profile atomically creates its initial journal named `Personal`.

### `questions`

| Field | Notes |
| --- | --- |
| `id` | Primary key |
| `journal_id` | Foreign key to `journals.id` |
| `label` | Current question label |
| `type` | Supported answer type |
| `position` | Display order within the journal |
| `is_active` | Whether shown for new/current entries |
| `created_at` | Creation timestamp |
| `updated_at` | Last update timestamp |

Supported initial types are short text, long text, yes/no, number, scale 1–5,
scale 1–10, time, select, and multi-select. Once answers exist, the question type
must not change in place. The old question is deactivated and a new one created.
Question positions are zero-based. Active questions are shown by position and ID;
reactivating a question appends it to the end of the active question list.

### `question_options`

| Field | Notes |
| --- | --- |
| `id` | Primary key |
| `question_id` | Foreign key to `questions.id` |
| `label` | Current option label |
| `position` | Display order within the question |
| `is_active` | Whether available for new/current answers |

Options belong to select or multi-select questions. Referenced options are
deactivated rather than deleted. Renaming an option changes the current
configuration; historical selected answers retain the earlier wording through
`answer_options.option_label_snapshot`.
Option positions are zero-based. Active options are shown by position and ID;
reactivating an option appends it to the end of the active option list.

### `days`

| Field | Notes |
| --- | --- |
| `id` | Primary key |
| `journal_id` | Foreign key to `journals.id` |
| `entry_date` | Calendar date in the journal/user timezone |
| `general_note` | General free-text note |
| `special_moment` | Free-text highlight of the day |
| `location` | Free-text location |
| `created_at` | Creation timestamp |
| `updated_at` | Last update timestamp |

`UNIQUE(journal_id, entry_date)` guarantees at most one entry per journal and
date. Saving a day, including its answers and related search update, is one
atomic SQLite transaction.

### `answers`

| Field | Notes |
| --- | --- |
| `id` | Primary key |
| `day_id` | Foreign key to `days.id` |
| `question_id` | Foreign key to `questions.id` |
| `text_value` | Nullable value for text answer types |
| `number_value` | Nullable value for numbers and scales |
| `bool_value` | Nullable value for yes/no |
| `time_value` | Nullable value for time |
| `question_label_snapshot` | Label presented when this answer was saved |
| `created_at` | Creation timestamp |
| `updated_at` | Last update timestamp |

`UNIQUE(day_id, question_id)` permits one answer per question on a day. Normally
only the typed value column relevant to the question is populated. Select and
multi-select values use `answer_options` rather than these scalar columns. Exact
database checks for value/type consistency should be settled with the migrations;
application validation is required in all cases.

The daily Save input distinguishes an omitted question from an explicitly cleared
question. Clearing deletes the answer row (and cascades to its selected options)
rather than storing an empty placeholder. Empty text values and empty select or
multi-select selections are also treated as clears.

### `answer_options`

| Field | Notes |
| --- | --- |
| `answer_id` | Foreign key to `answers.id` |
| `option_id` | Foreign key to `question_options.id` |
| `option_label_snapshot` | Option label presented when this answer was saved |

This join table represents selected options for both select and multi-select
answers. The migration should prevent the same option being attached to the same
answer more than once. `option_label_snapshot` preserves the wording of each
selected option independently of later renames. Select questions additionally
require validation that no more than one option is attached.

### `photos`

| Field | Notes |
| --- | --- |
| `id` | Primary key |
| `day_id` | Foreign key to `days.id` |
| `relative_path` | Path relative to the configured data/photo root |
| `original_filename` | Client-provided filename, retained as metadata only |
| `mime_type` | Validated media type |
| `file_size` | Stored file size in bytes |
| `created_at` | Creation timestamp |

Image bytes are never stored as SQLite BLOBs. A representative layout is:

```text
data/photos/USER_ID/YYYY/MM/DD/UUID.jpg
```

The database and photos directory together form the durable journal backup.
Paths stored in the database remain relative so the data directory can move.

## Small technical tables

- `sessions` stores minimal server-side session state for local profiles. Bearer
  tokens contain 256 bits of cryptographically secure randomness and only their
  SHA-256 hashes are stored in `token_hash`. Sessions have a fixed UTC expiration;
  lookup neither extends it nor accepts expired sessions. Expired rows can be
  removed opportunistically.
- `schema_migrations` records applied database migrations.
- `search_documents` holds one derived searchable document per day.
- An SQLite FTS virtual table indexes the derived search documents.

These tables should remain small and purpose-specific.

## Historical integrity

Questions and options referenced by historical answers should not normally be
physically deleted. They are deactivated instead, which removes them from normal
future entry without breaking references. Renaming a question or option changes
the current journal configuration but does not rewrite historical wording:
`answers.question_label_snapshot` preserves the question label and
`answer_options.option_label_snapshot` preserves each selected option label as
they existed when the answer was saved.

Editing an existing historical answer preserves its question label snapshot.
Likewise, a selected option that remains selected preserves its option label
snapshot; only newly added selections capture the option's current label. An
inactive question may be edited or cleared when that day already has its answer,
but it cannot receive a new answer. An inactive option already selected on that
answer may remain selected or be removed, but cannot be newly selected.

The initial migration cascades deletion of owned records from users through
journals and their days, and from days through answers and photos. Sessions are
also owned by users. Deleting an answer cascades its selected `answer_options`.
Direct deletion of a question or option referenced by a historical answer is
prevented. This allows removal of an entire owning container while protecting
historical meaning from isolated configuration deletion.

## Full-text search

Search covers at least text answers, general note, special moment, and location.
The preferred design builds one normalized search document for each day and
indexes it with SQLite FTS. Search tables contain derived data only: they can be
discarded and rebuilt completely from `days`, `answers`, `questions`, and related
option data. FTS must never become the only copy of journal content.
