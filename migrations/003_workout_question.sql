CREATE TEMP TABLE questions_backup AS SELECT * FROM questions;
CREATE TEMP TABLE question_options_backup AS SELECT * FROM question_options;
CREATE TEMP TABLE answers_backup AS SELECT * FROM answers;
CREATE TEMP TABLE answer_options_backup AS SELECT * FROM answer_options;

DROP TABLE answer_options;
DROP TABLE answers;
DROP TABLE question_options;
DROP TABLE questions;

CREATE TABLE questions (
    id INTEGER PRIMARY KEY,
    journal_id INTEGER NOT NULL REFERENCES journals(id) ON DELETE CASCADE,
    label TEXT NOT NULL CHECK (length(trim(label)) > 0),
    type TEXT NOT NULL CHECK (type IN (
        'short_text', 'long_text', 'workout', 'boolean', 'number', 'scale_5',
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

INSERT INTO questions SELECT * FROM questions_backup;
INSERT INTO question_options SELECT * FROM question_options_backup;
INSERT INTO answers SELECT * FROM answers_backup;
INSERT INTO answer_options SELECT * FROM answer_options_backup;

DROP TABLE questions_backup;
DROP TABLE question_options_backup;
DROP TABLE answers_backup;
DROP TABLE answer_options_backup;
