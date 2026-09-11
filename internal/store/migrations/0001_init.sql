-- 0001: initial schema (spec §4)
--
-- snippets.rowid is the surrogate FTS rowid and snippets.id is the ULID
-- exposed by the API. snippets.tags mirrors the space-joined tag names
-- and snippets.body_text mirrors the plaintext body, because
-- external-content FTS5 reads every FTS column from the main table on
-- delete and rebuild and cannot index the BLOB body column.

CREATE TABLE folders (
  id         TEXT PRIMARY KEY,
  parent_id  TEXT REFERENCES folders(id),
  name       TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  deleted_at TEXT
);

CREATE TABLE snippets (
  rowid        INTEGER PRIMARY KEY,
  id           TEXT NOT NULL UNIQUE,
  title        TEXT NOT NULL,
  body         BLOB NOT NULL,
  body_text    TEXT NOT NULL DEFAULT '',
  language     TEXT NOT NULL DEFAULT '',
  notes        TEXT NOT NULL DEFAULT '',
  tags         TEXT NOT NULL DEFAULT '',
  folder_id    TEXT REFERENCES folders(id),
  is_sensitive INTEGER NOT NULL DEFAULT 0,
  created_at   TEXT NOT NULL,
  updated_at   TEXT NOT NULL,
  deleted_at   TEXT
);

CREATE TABLE tags (
  id   INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE
);

CREATE TABLE snippet_tags (
  snippet_id TEXT NOT NULL REFERENCES snippets(id),
  tag_id     INTEGER NOT NULL REFERENCES tags(id),
  PRIMARY KEY (snippet_id, tag_id)
);

CREATE VIRTUAL TABLE snippets_fts USING fts5 (
  title, notes, body_text, tags,
  content='snippets', content_rowid='rowid',
  tokenize='unicode61'
);

CREATE INDEX idx_snippets_folder ON snippets(folder_id);
CREATE INDEX idx_snippets_updated ON snippets(updated_at);
CREATE INDEX idx_snippets_deleted ON snippets(deleted_at);
CREATE INDEX idx_folders_parent ON folders(parent_id);
CREATE INDEX idx_folders_deleted ON folders(deleted_at);
