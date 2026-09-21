CREATE TABLE snippet_revisions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  snippet_id TEXT NOT NULL REFERENCES snippets(id) ON DELETE CASCADE,
  saved_at TEXT NOT NULL,
  version_at TEXT NOT NULL,
  encrypted INTEGER NOT NULL DEFAULT 0,
  payload BLOB NOT NULL
);
CREATE INDEX idx_revisions_snippet ON snippet_revisions(snippet_id, id);
