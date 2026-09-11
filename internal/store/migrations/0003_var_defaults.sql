-- Spec §4: per-variable defaults for template snippets (the
-- `uses_variables` flag's persisted companion).
--
-- var_defaults holds the JSON {"name": value} map for non-sensitive
-- rows ('{}' when the snippet has no defaults). For sensitive rows the
-- plain column is '' (a default can be as sensitive as the body) and
-- var_defaults_enc holds the same JSON map sealed with AES-256-GCM
-- under the snippet id, the same scheme as body; it is NULL when a
-- sensitive snippet has no defaults. Neither column is FTS-indexed, so
-- the external-content FTS write discipline is unaffected.
ALTER TABLE snippets ADD COLUMN var_defaults TEXT NOT NULL DEFAULT '';
ALTER TABLE snippets ADD COLUMN var_defaults_enc BLOB;
