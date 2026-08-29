CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    pin_hash TEXT,
    timezone TEXT NOT NULL DEFAULT 'UTC' CHECK (length(trim(timezone)) > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE journals (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX journals_user_id_idx ON journals(user_id);

CREATE TABLE questions (
    id INTEGER PRIMARY KEY,
    journal_id INTEGER NOT NULL REFERENCES journals(id) ON DELETE CASCADE,
    label TEXT NOT NULL CHECK (length(trim(label)) > 0),
    type TEXT NOT NULL CHECK (type IN (
        'short_text', 'long_text', 'boolean', 'number', 'scale_5',
        'scale_10', 'time', 'select', 'multi_select'
    )),
    position INTEGER NOT NULL CHECK (position >= 0),
    is_active INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX questions_journal_id_idx ON questions(journal_id);

CREATE TABLE question_options (
    id INTEGER PRIMARY KEY,
    question_id INTEGER NOT NULL REFERENCES questions(id) ON DELETE CASCADE,
    label TEXT NOT NULL CHECK (length(trim(label)) > 0),
    position INTEGER NOT NULL CHECK (position >= 0),
    is_active INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1))
);
CREATE INDEX question_options_question_id_idx ON question_options(question_id);

CREATE TABLE days (
    id INTEGER PRIMARY KEY,
    journal_id INTEGER NOT NULL REFERENCES journals(id) ON DELETE CASCADE,
    entry_date TEXT NOT NULL,
    general_note TEXT NOT NULL DEFAULT '',
    special_moment TEXT NOT NULL DEFAULT '',
    location TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (journal_id, entry_date)
);

CREATE TABLE answers (
    id INTEGER PRIMARY KEY,
    day_id INTEGER NOT NULL REFERENCES days(id) ON DELETE CASCADE,
    question_id INTEGER NOT NULL REFERENCES questions(id) ON DELETE NO ACTION,
    text_value TEXT,
    number_value REAL,
    bool_value INTEGER CHECK (bool_value IS NULL OR bool_value IN (0, 1)),
    time_value TEXT,
    question_label_snapshot TEXT NOT NULL CHECK (length(trim(question_label_snapshot)) > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (day_id, question_id)
);
CREATE INDEX answers_question_id_idx ON answers(question_id);

CREATE TABLE answer_options (
    answer_id INTEGER NOT NULL REFERENCES answers(id) ON DELETE CASCADE,
    option_id INTEGER NOT NULL REFERENCES question_options(id) ON DELETE NO ACTION,
    option_label_snapshot TEXT NOT NULL CHECK (length(trim(option_label_snapshot)) > 0),
    PRIMARY KEY (answer_id, option_id)
);
CREATE INDEX answer_options_option_id_idx ON answer_options(option_id);

CREATE TABLE photos (
    id INTEGER PRIMARY KEY,
    day_id INTEGER NOT NULL REFERENCES days(id) ON DELETE CASCADE,
    relative_path TEXT NOT NULL CHECK (length(trim(relative_path)) > 0),
    original_filename TEXT NOT NULL,
    mime_type TEXT NOT NULL CHECK (length(trim(mime_type)) > 0),
    file_size INTEGER NOT NULL CHECK (file_size >= 0),
    created_at TEXT NOT NULL
);
CREATE INDEX photos_day_id_idx ON photos(day_id);

CREATE TABLE sessions (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX sessions_user_id_idx ON sessions(user_id);
