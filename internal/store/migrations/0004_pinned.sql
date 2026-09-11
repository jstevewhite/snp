-- Spec §4: the pinned ("favorite") flag behind the app's Favorites list.
--
-- INTEGER 0/1 like the other booleans. A pin is not sensitive content, so
-- it is stored plainly for every row — including sensitive snippets, whose
-- body and var_defaults are encrypted — and it is not FTS-indexed, so the
-- external-content FTS write discipline (insertFTSTx/deleteFTSTx) is
-- unaffected. Old rows default to 0.
ALTER TABLE snippets ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0;
