package store

import (
	"context"
	"database/sql"
)

// SyncOut is the /api/sync response (spec §5).
type SyncOut struct {
	ServerTime string `json:"server_time"`
	Folders    []any  `json:"folders"`
	Snippets   []any  `json:"snippets"`
}

// Tombstone marks a deleted row in a sync response.
type Tombstone struct {
	ID        string `json:"id"`
	DeletedAt string `json:"deleted_at"`
}

// SyncSince returns folders and snippets updated or deleted after
// since, plus server_time captured BEFORE the read (spec §5). An empty
// since is a full sync.
//
// Boundary: server_time and the stored timestamps are whole seconds
// (spec §4), and the filters use >= so a row written in the same second
// as the previous snapshot — whose updated_at then equals that
// server_time — is still sent on the next sync instead of being skipped
// forever by a strict >. Duplicates are harmless: the client merge is
// idempotent (spec §5), and server_time advances each sync, so a row is
// resent at most once.
func (s *Store) SyncSince(since string) (SyncOut, error) {
	ctx := context.Background()
	// Captured before the read: a row written just after the snapshot
	// has updated_at >= server_time and is picked up by the next sync.
	out := SyncOut{
		ServerTime: s.now(),
		Folders:    []any{},
		Snippets:   []any{},
	}

	// A full sync returns every folder (live and deleted); an incremental
	// sync returns only those updated or deleted after since.
	fq := `SELECT id, parent_id, name, created_at, updated_at, deleted_at FROM folders`
	var fargs []any
	if since != "" {
		fq += ` WHERE updated_at >= ? OR deleted_at >= ?`
		fargs = append(fargs, since, since)
	}
	fq += ` ORDER BY updated_at`
	frows, err := s.db.QueryContext(ctx, fq, fargs...)
	if err != nil {
		return out, err
	}
	for frows.Next() {
		var f Folder
		var parent sql.NullString
		if err := frows.Scan(&f.ID, &parent, &f.Name, &f.CreatedAt, &f.UpdatedAt, &f.DeletedAt); err != nil {
			frows.Close()
			return out, err
		}
		if parent.Valid {
			f.ParentID = &parent.String
		}
		if f.DeletedAt != nil {
			out.Folders = append(out.Folders, Tombstone{ID: f.ID, DeletedAt: *f.DeletedAt})
		} else {
			out.Folders = append(out.Folders, f)
		}
	}
	if err := frows.Err(); err != nil {
		frows.Close()
		return out, err
	}
	frows.Close()

	sq := `SELECT id, title, body, language, notes, folder_id, is_sensitive, uses_variables,
		var_defaults, var_defaults_enc, created_at, updated_at FROM snippets`
	var sargs []any
	if since != "" {
		sq += ` WHERE deleted_at IS NULL AND updated_at >= ?`
		sargs = append(sargs, since)
	} else {
		sq += ` WHERE deleted_at IS NULL`
	}
	sq += ` ORDER BY updated_at`
	srows, err := s.db.QueryContext(ctx, sq, sargs...)
	if err != nil {
		return out, err
	}
	live := []SnippetOut{}
	var ids []string
	for srows.Next() {
		var sn SnippetOut
		var body []byte
		var folder sql.NullString
		var sens, uvars int
		var vdPlain string
		var vdEnc sql.Null[[]byte]
		if err := srows.Scan(&sn.ID, &sn.Title, &body, &sn.Language, &sn.Notes,
			&folder, &sens, &uvars, &vdPlain, &vdEnc, &sn.CreatedAt, &sn.UpdatedAt); err != nil {
			srows.Close()
			return out, err
		}
		if folder.Valid {
			sn.FolderID = &folder.String
		}
		sn.IsSensitive = sens != 0
		sn.UsesVariables = uvars != 0
		if !sn.IsSensitive {
			b := string(body)
			sn.Body = &b
			vd, err := s.loadVarDefaults(sn.ID, vdPlain, nil, false)
			if err != nil {
				srows.Close()
				return out, err
			}
			sn.VarDefaults = vd
		}
		live = append(live, sn)
		ids = append(ids, sn.ID)
	}
	if err := srows.Err(); err != nil {
		srows.Close()
		return out, err
	}
	srows.Close()

	tagMap, err := s.tagsFor(ctx, ids)
	if err != nil {
		return out, err
	}
	for i := range live {
		live[i].Tags = tagMap[live[i].ID]
	}
	for _, sn := range live {
		out.Snippets = append(out.Snippets, sn)
	}

	// Tombstones: a full sync includes every soft-deleted snippet; an
	// incremental sync includes those deleted after since.
	tq := `SELECT id, deleted_at FROM snippets WHERE deleted_at IS NOT NULL`
	var targs []any
	if since != "" {
		tq += ` AND deleted_at >= ?`
		targs = append(targs, since)
	}
	tq += ` ORDER BY deleted_at`
	trows, err := s.db.QueryContext(ctx, tq, targs...)
	if err != nil {
		return out, err
	}
	for trows.Next() {
		var id, del string
		if err := trows.Scan(&id, &del); err != nil {
			trows.Close()
			return out, err
		}
		out.Snippets = append(out.Snippets, Tombstone{ID: id, DeletedAt: del})
	}
	if err := trows.Err(); err != nil {
		trows.Close()
		return out, err
	}
	trows.Close()

	return out, nil
}
