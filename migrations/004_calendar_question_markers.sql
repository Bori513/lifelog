ALTER TABLE questions ADD COLUMN calendar_marker TEXT NOT NULL DEFAULT ''
    CHECK (length(calendar_marker) <= 16);
