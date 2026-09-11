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

// Folder is a (possibly soft-deleted) folder.
type Folder struct {
	ID        string  `json:"id"`
	ParentID  *string `json:"parent_id"`
	Name      string  `json:"name"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
	DeletedAt *string `json:"deleted_at,omitempty"`
}

// ListFolders returns live folders, ordered by name.
func (s *Store) ListFolders() ([]Folder, error) {
	ctx := context.Background()
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, parent_id, name, created_at, updated_at
		 FROM folders WHERE deleted_at IS NULL ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Folder{}
	for rows.Next() {
		var f Folder
		var parent sql.NullString
		if err := rows.Scan(&f.ID, &parent, &f.Name, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		if parent.Valid {
			f.ParentID = &parent.String
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// CreateFolder creates a folder; the name must be unique among its
// siblings (case-insensitive).
func (s *Store) CreateFolder(name string, parentID *string) (Folder, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Folder{}, fmt.Errorf("%w: folder name required", ErrInvalid)
	}
	ctx := context.Background()
	now := s.now()
	id := ulid.MustNew(ulid.Now(), rand.Reader).String()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Folder{}, err
	}
	defer tx.Rollback()

	if parentID != nil {
		if err := liveFolderTx(tx, *parentID); err != nil {
			return Folder{}, err
		}
	}
	if err := uniqueSiblingTx(ctx, tx, parentID, name, ""); err != nil {
		return Folder{}, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO folders (id, parent_id, name, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		id, parentID, name, now, now); err != nil {
		return Folder{}, err
	}
	if err := tx.Commit(); err != nil {
		return Folder{}, err
	}
	return Folder{ID: id, ParentID: parentID, Name: name, CreatedAt: now, UpdatedAt: now}, nil
}

// UpdateFolder renames and/or moves a live folder. Moving under its own
// descendant is refused (ErrFolderCycle), moving under a missing or
// soft-deleted folder is refused (ErrNotFound — the destination must be
// live, as on create), and renaming onto — or moving into — a sibling
// name collision is refused (ErrNameTaken).
func (s *Store) UpdateFolder(id string, name *string, parentID *string) (Folder, error) {
	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Folder{}, err
	}
	defer tx.Rollback()

	var f Folder
	var curParent sql.NullString
	err = tx.QueryRowContext(ctx,
		`SELECT id, parent_id, name, created_at FROM folders WHERE id = ? AND deleted_at IS NULL`, id).
		Scan(&f.ID, &curParent, &f.Name, &f.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Folder{}, ErrNotFound
	}
	if err != nil {
		return Folder{}, err
	}

	// The effective destination after this update: an explicit parentID,
	// or the current parent when the folder is not being moved. (nil
	// parentID means "no change", so a folder at the root is renamed with
	// newParent == nil.)
	newParent := parentID
	if newParent == nil {
		if curParent.Valid {
			newParent = &curParent.String
		}
	}
	moved := false
	if parentID != nil {
		cur := ""
		if curParent.Valid {
			cur = curParent.String
		}
		moved = *parentID != cur
	}
	if newParent != nil {
		// The destination must be a live folder. Moving under a missing
		// or soft-deleted one would orphan this folder under a tombstone:
		// it disappears from every UI tree, and the tombstoned parent then
		// blocks the daily purge with a foreign-key error forever.
		// CreateFolder enforces the same rule.
		if err := liveFolderTx(tx, *newParent); err != nil {
			return Folder{}, err
		}
		if *newParent == id {
			return Folder{}, ErrFolderCycle
		}
		// Walk up from the new parent; reaching id means a cycle.
		for cur := *newParent; cur != ""; {
			var p sql.NullString
			err := tx.QueryRowContext(ctx,
				`SELECT parent_id FROM folders WHERE id = ? AND deleted_at IS NULL`, cur).Scan(&p)
			if errors.Is(err, sql.ErrNoRows) {
				// The live destination's own ancestor chain is broken (a
				// parent above it is deleted or missing — orphaned data).
				// Moving under it would deepen the damage, so refuse.
				return Folder{}, ErrFolderCycle
			}
			if err != nil {
				return Folder{}, err
			}
			if !p.Valid {
				break
			}
			if p.String == id {
				return Folder{}, ErrFolderCycle
			}
			cur = p.String
		}
	}
	if name != nil {
		*name = strings.TrimSpace(*name)
		if *name == "" {
			return Folder{}, fmt.Errorf("%w: folder name required", ErrInvalid)
		}
		f.Name = *name
	}
	// The name must stay unique among the effective parent's children: on
	// rename as before, and on a move that changes the parent — which
	// previously skipped the check and could create duplicate siblings
	// (breaking the spec §4 "unique per parent" invariant that import
	// folder_path relies on).
	if name != nil || moved {
		if err := uniqueSiblingTx(ctx, tx, newParent, f.Name, id); err != nil {
			return Folder{}, err
		}
	}
	f.ParentID = newParent

	now := s.now()
	if _, err := tx.ExecContext(ctx,
		`UPDATE folders SET name = ?, parent_id = ?, updated_at = ? WHERE id = ?`,
		f.Name, f.ParentID, now, id); err != nil {
		return Folder{}, err
	}
	if err := tx.Commit(); err != nil {
		return Folder{}, err
	}
	f.UpdatedAt = now
	return f, nil
}

// DeleteFolder soft-deletes a folder; refused while it has live
// children or live snippets.
func (s *Store) DeleteFolder(id string) error {
	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var n int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM folders WHERE id = ? AND deleted_at IS NULL`, id).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM folders WHERE parent_id = ? AND deleted_at IS NULL`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrFolderNotEmpty
	}
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM snippets WHERE folder_id = ? AND deleted_at IS NULL`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrFolderNotEmpty
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE folders SET deleted_at = ? WHERE id = ?`, s.now(), id); err != nil {
		return err
	}
	return tx.Commit()
}

// uniqueSiblingTx fails with ErrNameTaken if a live sibling with the
// same name (case-insensitive) exists. excludeID skips itself.
func uniqueSiblingTx(ctx context.Context, tx *sql.Tx, parent *string, name, excludeID string) error {
	var q string
	var args []any
	if parent == nil {
		q = `SELECT COUNT(*) FROM folders WHERE parent_id IS NULL AND name = ? COLLATE NOCASE AND deleted_at IS NULL`
		args = []any{name}
	} else {
		q = `SELECT COUNT(*) FROM folders WHERE parent_id = ? AND name = ? COLLATE NOCASE AND deleted_at IS NULL`
		args = []any{*parent, name}
	}
	if excludeID != "" {
		q += ` AND id != ?`
		args = append(args, excludeID)
	}
	var n int
	if err := tx.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrNameTaken
	}
	return nil
}
