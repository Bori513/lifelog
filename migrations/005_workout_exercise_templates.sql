CREATE TABLE workout_exercise_templates (
    id INTEGER PRIMARY KEY,
    question_id INTEGER NOT NULL REFERENCES questions(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(trim(name)) > 0 AND length(name) <= 100),
    position INTEGER NOT NULL CHECK (position >= 0),
    created_at TEXT NOT NULL,
    UNIQUE (question_id, name COLLATE NOCASE)
);
CREATE INDEX workout_exercise_templates_question_id_idx
    ON workout_exercise_templates(question_id);
