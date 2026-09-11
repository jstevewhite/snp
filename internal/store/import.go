package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/oklog/ulid"
)

// ImportFolder is a folder entry in an import document.
type ImportFolder struct {
	ID       string  `json:"id"`
	ParentID *string `json:"parent_id"`
	Name     string  `json:"name"`
}

// ImportSnippet is a snippet entry in an import document. Bodies are
// always plaintext; sensitive snippets are re-encrypted on import.
type ImportSnippet struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Body        string   `json:"body"`
	Language    string   `json:"language"`
	Notes       string   `json:"notes"`
	FolderID    *string  `json:"folder_id"`
	FolderPath  string   `json:"folder_path"`
	Tags        []string `json:"tags"`
	IsSensitive bool     `json:"is_sensitive"`
	// UsesVariables marks the body as a template (spec §4); older
	// export docs without the field decode as false.
	UsesVariables bool `json:"uses_variables"`
	// VarDefaults holds per-variable default values (spec §4); older
	// export docs without the field decode as empty.
	VarDefaults map[string]string `json:"var_defaults"`
	CreatedAt   string            `json:"created_at"`
	UpdatedAt   string            `json:"updated_at"`
}

// ImportDoc is the import document (spec §5). It accepts the export
// shape plus folder_path.
type ImportDoc struct {
	Version    int             `json:"version"`
	ExportedAt string          `json:"exported_at,omitempty"`
	Folders    []ImportFolder  `json:"folders"`
	Snippets   []ImportSnippet `json:"snippets"`
}

// ImportResult counts rows created vs updated.
type ImportResult struct {
	Created int `json:"created"`
	Updated int `json:"updated"`
}

// Import applies doc with mode "merge" (upsert by id) or "replace"
// (upsert everything in the doc, then soft-delete live rows not in it),
// in a single transaction.
func (s *Store) Import(doc ImportDoc, mode string) (ImportResult, error) {
	if doc.Version != 1 {
		return ImportResult{}, fmt.Errorf("%w: unsupported version %d", ErrImport, doc.Version)
	}
	if mode != "merge" && mode != "replace" {
		return ImportResult{}, fmt.Errorf("%w: mode must be merge or replace", ErrImport)
	}
	seen := map[string]bool{}
	for _, sn := range doc.Snippets {
		if sn.ID != "" {
			if seen[sn.ID] {
				return ImportResult{}, fmt.Errorf("%w: duplicate id %q", ErrImport, sn.ID)
			}
			seen[sn.ID] = true
		}
	}

	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ImportResult{}, err
	}
	defer tx.Rollback()
	now := s.now()

	for _, f := range doc.Folders {
		if f.ID == "" || strings.TrimSpace(f.Name) == "" {
			return ImportResult{}, fmt.Errorf("%w: folder with empty id or name", ErrImport)
		}
		name := strings.TrimSpace(f.Name)
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM folders WHERE id = ?`, f.ID).Scan(&n); err != nil {
			return ImportResult{}, err
		}
		if n > 0 {
			if _, err := tx.ExecContext(ctx,
				`UPDATE folders SET name = ?, parent_id = ?, updated_at = ?, deleted_at = NULL WHERE id = ?`,
				name, f.ParentID, now, f.ID); err != nil {
				return ImportResult{}, err
			}
		} else {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO folders (id, parent_id, name, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
				f.ID, f.ParentID, name, now, now); err != nil {
				return ImportResult{}, err
			}
		}
	}

	var res ImportResult
	keptIDs := make([]string, 0, len(doc.Snippets))
	for _, sn := range doc.Snippets {
		id, created, err := s.importSnippetTx(ctx, tx, sn, now)
		if err != nil {
			return ImportResult{}, err
		}
		keptIDs = append(keptIDs, id)
		if created {
			res.Created++
		} else {
			res.Updated++
		}
	}

	if mode == "replace" {
		if err := s.softDeleteMissingTx(ctx, tx, "snippets", keptIDs, now); err != nil {
			return ImportResult{}, err
		}
		folderIDs := make([]string, 0, len(doc.Folders))
		for _, f := range doc.Folders {
			folderIDs = append(folderIDs, f.ID)
		}
		if err := s.softDeleteMissingTx(ctx, tx, "folders", folderIDs, now); err != nil {
			return ImportResult{}, err
		}
		// Never orphan a folder that live snippets still reference.
		if _, err := tx.ExecContext(ctx,
			`UPDATE folders SET deleted_at = NULL
			 WHERE deleted_at = ?
			   AND id IN (SELECT folder_id FROM snippets WHERE deleted_at IS NULL AND folder_id IS NOT NULL)`,
			now); err != nil {
			return ImportResult{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return ImportResult{}, err
	}
	return res, nil
}

// softDeleteMissingTx soft-deletes live rows of table whose id is not
// in keep (which may be empty).
func (s *Store) softDeleteMissingTx(ctx context.Context, tx *sql.Tx, table string, keep []string, now string) error {
	var q string
	var args []any
	if len(keep) == 0 {
		q = `UPDATE ` + table + ` SET deleted_at = ? WHERE deleted_at IS NULL`
		args = []any{now}
	} else {
		q = `UPDATE ` + table + ` SET deleted_at = ? WHERE deleted_at IS NULL AND id NOT IN (` + placeholders(len(keep)) + `)`
		args = append([]any{now}, toAny(keep)...)
	}
	_, err := tx.ExecContext(ctx, q, args...)
	return err
}

// importSnippetTx upserts one snippet inside tx and returns its id and
// whether it was created.
func (s *Store) importSnippetTx(ctx context.Context, tx *sql.Tx, in ImportSnippet, now string) (string, bool, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return "", false, fmt.Errorf("%w: snippet with empty title", ErrImport)
	}
	for _, raw := range in.Tags {
		if !tagRe.MatchString(strings.ToLower(strings.TrimSpace(raw))) {
			return "", false, fmt.Errorf("%w: invalid tag %q", ErrImport, raw)
		}
	}
	id := in.ID
	if id == "" {
		id = ulid.MustNew(ulid.Now(), rand.Reader).String()
	}

	var folderID *string
	if in.FolderID != nil && *in.FolderID != "" {
		if err := liveFolderTx(tx, *in.FolderID); err != nil {
			return "", false, fmt.Errorf("%w: snippet %q references unknown folder %q", ErrImport, id, *in.FolderID)
		}
		folderID = in.FolderID
	} else if in.FolderPath != "" {
		fid, err := s.resolveFolderPathTx(ctx, tx, in.FolderPath)
		if err != nil {
			return "", false, err
		}
		folderID = &fid
	}

	var existingCreated sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT created_at FROM snippets WHERE id = ?`, id).Scan(&existingCreated)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", false, err
	}
	exists := existingCreated.Valid

	var createdTS, updatedTS string
	if in.CreatedAt != "" {
		createdTS = in.CreatedAt
	} else if exists {
		createdTS = existingCreated.String
	} else {
		createdTS = now
	}
	if in.UpdatedAt != "" {
		updatedTS = in.UpdatedAt
	} else {
		updatedTS = now
	}

	body, err := s.storeBody(id, in.Body, in.IsSensitive)
	if err != nil {
		return "", false, err
	}

	vdPlain, vdEnc, err := s.storeVarDefaults(id, in.VarDefaults, in.IsSensitive)
	if err != nil {
		return "", false, err
	}

	bodyText := in.Body
	if in.IsSensitive {
		bodyText = ""
	}
	var rowid int64
	if exists {
		if err := tx.QueryRowContext(ctx, `SELECT rowid FROM snippets WHERE id = ?`, id).Scan(&rowid); err != nil {
			return "", false, err
		}
		// Remove the old FTS row while the content row still holds the old
		// values; an external-content FTS5 delete reads them from the
		// content table. The rowid is already indexed because the snippet
		// exists.
		if err := s.deleteFTSTx(ctx, tx, rowid); err != nil {
			return "", false, err
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE snippets SET title = ?, body = ?, body_text = ?, language = ?, notes = ?, folder_id = ?,
			 is_sensitive = ?, uses_variables = ?, var_defaults = ?, var_defaults_enc = ?, created_at = ?, updated_at = ?, deleted_at = NULL
			 WHERE id = ?`,
			title, body, bodyText, in.Language, in.Notes, folderID, boolInt(in.IsSensitive),
			boolInt(in.UsesVariables), vdPlain, vdEnc, createdTS, updatedTS, id); err != nil {
			return "", false, err
		}
	} else {
		res, err := tx.ExecContext(ctx,
			`INSERT INTO snippets (id, title, body, body_text, language, notes, tags, folder_id,
		 is_sensitive, uses_variables, var_defaults, var_defaults_enc, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, '', ?, ?, ?, ?, ?, ?, ?)`,
			id, title, body, bodyText, in.Language, in.Notes, folderID, boolInt(in.IsSensitive),
			boolInt(in.UsesVariables), vdPlain, vdEnc, createdTS, updatedTS)
		if err != nil {
			return "", false, err
		}
		rowid, err = res.LastInsertId()
		if err != nil {
			return "", false, err
		}
	}
	tags, err := s.setTagsTx(ctx, tx, id, in.Tags)
	if err != nil {
		return "", false, err
	}
	if err := s.insertFTSTx(ctx, tx, rowid, title, in.Notes, bodyText, strings.Join(tags, " ")); err != nil {
		return "", false, err
	}
	return id, !exists, nil
}

// resolveFolderPathTx returns the id of the live folder at the given
// slash-separated path, creating missing segments.
func (s *Store) resolveFolderPathTx(ctx context.Context, tx *sql.Tx, path string) (string, error) {
	seg := strings.Trim(path, "/")
	if seg == "" {
		return "", fmt.Errorf("%w: empty folder_path", ErrImport)
	}
	parts := strings.Split(seg, "/")
	parent := ""
	var parentPtr *string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			return "", fmt.Errorf("%w: empty segment in folder_path %q", ErrImport, path)
		}
		var id string
		var err error
		if parent == "" {
			err = tx.QueryRowContext(ctx,
				`SELECT id FROM folders WHERE parent_id IS NULL AND name = ? COLLATE NOCASE AND deleted_at IS NULL`,
				p).Scan(&id)
		} else {
			err = tx.QueryRowContext(ctx,
				`SELECT id FROM folders WHERE parent_id = ? AND name = ? COLLATE NOCASE AND deleted_at IS NULL`,
				parent, p).Scan(&id)
		}
		if errors.Is(err, sql.ErrNoRows) {
			now := s.now()
			id = ulid.MustNew(ulid.Now(), rand.Reader).String()
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO folders (id, parent_id, name, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
				id, parentPtr, p, now, now); err != nil {
				return "", err
			}
		} else if err != nil {
			return "", err
		}
		parent = id
		parentPtr = &parent
	}
	return parent, nil
}
