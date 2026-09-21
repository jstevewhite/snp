package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// TrashEntry intentionally excludes bodies and template defaults.
type TrashEntry struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Language    string `json:"language"`
	IsSensitive bool   `json:"is_sensitive"`
	DeletedAt   string `json:"deleted_at"`
}

func (s *Store) ListTrash(limit, offset int) ([]TrashEntry, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.Query(`SELECT id, title, language, is_sensitive, deleted_at FROM snippets WHERE deleted_at IS NOT NULL ORDER BY deleted_at DESC, id DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TrashEntry{}
	for rows.Next() {
		var r TrashEntry
		if err := rows.Scan(&r.ID, &r.Title, &r.Language, &r.IsSensitive, &r.DeletedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func redactSnippet(out SnippetOut) SnippetOut {
	if out.IsSensitive {
		out.Body = nil
		out.VarDefaults = nil
	}
	return out
}

func (s *Store) RestoreSnippet(id string) (SnippetOut, error) {
	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SnippetOut{}, err
	}
	defer tx.Rollback()
	// Indexed content is unchanged: the retained FTS row is already correct.
	res, err := tx.ExecContext(ctx, `UPDATE snippets SET deleted_at = NULL, updated_at = ?, folder_id = CASE WHEN folder_id IN (SELECT id FROM folders WHERE deleted_at IS NULL) THEN folder_id ELSE NULL END WHERE id = ? AND deleted_at IS NOT NULL`, s.now(), id)
	if err != nil {
		return SnippetOut{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return SnippetOut{}, err
	}
	if n == 0 {
		return SnippetOut{}, ErrNotFound
	}
	out, err := s.getSnippet(ctx, tx, id, true)
	if err != nil {
		return SnippetOut{}, err
	}
	if err := tx.Commit(); err != nil {
		return SnippetOut{}, err
	}
	return redactSnippet(out), nil
}

// Revision summaries contain no historical content, even for plain versions.
type Revision struct {
	ID        int64  `json:"id"`
	SavedAt   string `json:"saved_at"`
	VersionAt string `json:"version_at"`
	Protected bool   `json:"protected"`
}

type RevisionDetail struct {
	Revision
	Snippet SnippetOut `json:"snippet"`
}

func snippetInput(out SnippetOut) SnippetInput {
	body := ""
	if out.Body != nil {
		body = *out.Body
	}
	return SnippetInput{Title: out.Title, Body: body, Language: out.Language, Notes: out.Notes, FolderID: out.FolderID, Tags: out.Tags, IsSensitive: out.IsSensitive, UsesVariables: out.UsesVariables, Pinned: out.Pinned, VarDefaults: out.VarDefaults}
}

func canonicalContent(in SnippetInput) SnippetInput {
	in.Pinned = false
	seen := map[string]bool{}
	for _, tag := range in.Tags {
		seen[strings.ToLower(strings.TrimSpace(tag))] = true
	}
	in.Tags = make([]string, 0, len(seen))
	for tag := range seen {
		in.Tags = append(in.Tags, tag)
	}
	sort.Strings(in.Tags)
	if len(in.VarDefaults) == 0 {
		in.VarDefaults = nil
	}
	return in
}

func revisionAAD(snippetID string, id int64) string {
	return fmt.Sprintf("revision:%s:%d", snippetID, id)
}

// captureRevisionTx runs before replacing the main row, including import upserts.
func (s *Store) captureRevisionTx(ctx context.Context, tx *sql.Tx, id string, next SnippetInput) error {
	old, err := s.getSnippet(ctx, tx, id, false)
	if err != nil {
		return err
	}
	if next.IsSensitive {
		if err := s.protectRevisionsTx(ctx, tx, id); err != nil {
			return err
		}
	}
	if reflect.DeepEqual(canonicalContent(snippetInput(old)), canonicalContent(next)) {
		return nil
	}
	payload, err := json.Marshal(old)
	if err != nil {
		return err
	}
	protected := old.IsSensitive || next.IsSensitive
	res, err := tx.ExecContext(ctx, `INSERT INTO snippet_revisions (snippet_id,saved_at,version_at,encrypted,payload) VALUES (?,?,?,?,?)`, id, s.now(), old.UpdatedAt, boolInt(protected), []byte{})
	if err != nil {
		return err
	}
	rid, err := res.LastInsertId()
	if err != nil {
		return err
	}
	if protected {
		key, err := s.requireKey()
		if err != nil {
			return err
		}
		payload, err = key.Seal(revisionAAD(id, rid), payload)
		if err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE snippet_revisions SET payload=? WHERE id=?`, payload, rid); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM snippet_revisions WHERE snippet_id=? AND id NOT IN (SELECT id FROM snippet_revisions WHERE snippet_id=? ORDER BY id DESC LIMIT 50)`, id, id)
	return err
}

func (s *Store) protectRevisionsTx(ctx context.Context, tx *sql.Tx, id string) error {
	rows, err := tx.QueryContext(ctx, `SELECT id,payload FROM snippet_revisions WHERE snippet_id=? AND encrypted=0`, id)
	if err != nil {
		return err
	}
	type entry struct {
		id      int64
		payload []byte
	}
	pending := []entry{}
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.id, &e.payload); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil
	}
	key, err := s.requireKey()
	if err != nil {
		return err
	}
	for _, e := range pending {
		enc, err := key.Seal(revisionAAD(id, e.id), e.payload)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE snippet_revisions SET payload=?,encrypted=1 WHERE id=?`, enc, e.id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ListRevisions(id string) ([]Revision, error) {
	var exists int
	if err := s.db.QueryRow(`SELECT 1 FROM snippets WHERE id=? AND deleted_at IS NULL`, id).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	rows, err := s.db.Query(`SELECT id,saved_at,version_at,encrypted FROM snippet_revisions WHERE snippet_id=? ORDER BY id DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Revision{}
	for rows.Next() {
		var r Revision
		if err := rows.Scan(&r.ID, &r.SavedAt, &r.VersionAt, &r.Protected); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) getRevision(ctx context.Context, q snippetReader, id string, rid int64) (RevisionDetail, error) {
	var r RevisionDetail
	var payload []byte
	err := q.QueryRowContext(ctx, `SELECT r.id,r.saved_at,r.version_at,r.encrypted,r.payload FROM snippet_revisions r JOIN snippets s ON s.id=r.snippet_id WHERE r.snippet_id=? AND r.id=? AND s.deleted_at IS NULL`, id, rid).Scan(&r.ID, &r.SavedAt, &r.VersionAt, &r.Protected, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	if r.Protected {
		key, err := s.requireKey()
		if err != nil {
			return r, err
		}
		payload, err = key.Open(revisionAAD(id, rid), payload)
		if err != nil {
			return r, err
		}
	}
	err = json.Unmarshal(payload, &r.Snippet)
	return r, err
}

func (s *Store) GetRevision(id string, rid int64) (RevisionDetail, error) {
	return s.getRevision(context.Background(), s.db, id, rid)
}

func (s *Store) RestoreRevision(id string, rid int64) (SnippetOut, error) {
	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SnippetOut{}, err
	}
	defer tx.Rollback()
	r, err := s.getRevision(ctx, tx, id, rid)
	if err != nil {
		return SnippetOut{}, err
	}
	current, err := s.getSnippet(ctx, tx, id, true)
	if err != nil {
		return SnippetOut{}, err
	}
	in := snippetInput(r.Snippet)
	in.Pinned = current.Pinned
	// Restoring history must never silently publish previously protected content.
	in.IsSensitive = current.IsSensitive || in.IsSensitive || r.Protected
	if in.FolderID != nil {
		if err := liveFolderTx(tx, *in.FolderID); errors.Is(err, ErrNotFound) {
			in.FolderID = nil
		} else if err != nil {
			return SnippetOut{}, err
		}
	}
	out, err := s.replaceSnippetTx(ctx, tx, id, in)
	if err != nil {
		return SnippetOut{}, err
	}
	if err := tx.Commit(); err != nil {
		return SnippetOut{}, err
	}
	return out, nil
}
