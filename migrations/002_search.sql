CREATE TABLE search_documents (
    day_id INTEGER PRIMARY KEY REFERENCES days(id) ON DELETE CASCADE,
    journal_id INTEGER NOT NULL REFERENCES journals(id) ON DELETE CASCADE,
    entry_date TEXT NOT NULL,
    body TEXT NOT NULL
);
CREATE INDEX search_documents_journal_date_idx
    ON search_documents(journal_id, entry_date DESC, day_id DESC);

CREATE VIRTUAL TABLE search_fts USING fts5(
    body,
    content='search_documents',
    content_rowid='day_id'
);

CREATE TRIGGER search_documents_insert AFTER INSERT ON search_documents BEGIN
    INSERT INTO search_fts(rowid, body) VALUES (new.day_id, new.body);
END;
CREATE TRIGGER search_documents_delete AFTER DELETE ON search_documents BEGIN
    INSERT INTO search_fts(search_fts, rowid, body) VALUES ('delete', old.day_id, old.body);
END;
CREATE TRIGGER search_documents_update AFTER UPDATE ON search_documents BEGIN
    INSERT INTO search_fts(search_fts, rowid, body) VALUES ('delete', old.day_id, old.body);
    INSERT INTO search_fts(rowid, body) VALUES (new.day_id, new.body);
END;
