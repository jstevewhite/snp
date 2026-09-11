-- Spec §4: per-snippet flag marking a body as a template. The server
-- treats the body as opaque text; the flag only controls frontend
-- behavior (variable panel in the detail view, rendered copy).
ALTER TABLE snippets ADD COLUMN uses_variables INTEGER NOT NULL DEFAULT 0;
