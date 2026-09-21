package store

import (
	"context"
	"database/sql"
	"strings"
)

// ExportDoc is the export document (spec §5). Bodies are plaintext.
type ExportDoc struct {
	Version    int          `json:"version"`
	ExportedAt string       `json:"exported_at"`
	Folders    []Folder     `json:"folders"`
	Snippets   []SnippetOut `json:"snippets"`
}

// Export returns all live folders and snippets, bodies decrypted.
func (s *Store) Export() (ExportDoc, error) {
	ctx := context.Background()
	doc := ExportDoc{
		Version:    1,
		ExportedAt: s.now(),
		Folders:    []Folder{},
		Snippets:   []SnippetOut{},
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return doc, err
	}
	defer tx.Rollback()
	frows, err := tx.QueryContext(ctx,
		`SELECT id, parent_id, name, created_at, updated_at
		 FROM folders WHERE deleted_at IS NULL ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return doc, err
	}
	for frows.Next() {
		var f Folder
		var parent sql.NullString
		if err := frows.Scan(&f.ID, &parent, &f.Name, &f.CreatedAt, &f.UpdatedAt); err != nil {
			frows.Close()
			return doc, err
		}
		if parent.Valid {
			f.ParentID = &parent.String
		}
		doc.Folders = append(doc.Folders, f)
	}
	if err := frows.Err(); err != nil {
		frows.Close()
		return doc, err
	}
	frows.Close()

	srows, err := tx.QueryContext(ctx,
		`SELECT id, title, body, language, notes, folder_id, is_sensitive, uses_variables, pinned,
		 var_defaults, var_defaults_enc, created_at, updated_at, tags
		 FROM snippets WHERE deleted_at IS NULL ORDER BY updated_at DESC`)
	if err != nil {
		return doc, err
	}
	for srows.Next() {
		var sn SnippetOut
		var body []byte
		var folder sql.NullString
		var sens, uvars, pinned int
		var vdPlain string
		var tags string
		var vdEnc sql.Null[[]byte]
		if err := srows.Scan(&sn.ID, &sn.Title, &body, &sn.Language, &sn.Notes,
			&folder, &sens, &uvars, &pinned, &vdPlain, &vdEnc, &sn.CreatedAt, &sn.UpdatedAt, &tags); err != nil {
			srows.Close()
			return doc, err
		}
		if folder.Valid {
			sn.FolderID = &folder.String
		}
		sn.IsSensitive = sens != 0
		sn.UsesVariables = uvars != 0
		sn.Pinned = pinned != 0
		if sn.IsSensitive {
			k, err := s.requireKey()
			if err != nil {
				srows.Close()
				return doc, err
			}
			pt, err := k.Open(sn.ID, body)
			if err != nil {
				srows.Close()
				return doc, err
			}
			plain := string(pt)
			sn.Body = &plain
		} else {
			b := string(body)
			sn.Body = &b
		}
		var encBytes []byte
		if vdEnc.Valid {
			encBytes = vdEnc.V
		}
		vd, err := s.loadVarDefaults(sn.ID, vdPlain, encBytes, sn.IsSensitive)
		if err != nil {
			srows.Close()
			return doc, err
		}
		sn.VarDefaults = vd
		doc.Snippets = append(doc.Snippets, sn)
		doc.Snippets[len(doc.Snippets)-1].Tags = strings.Fields(tags)
	}
	if err := srows.Err(); err != nil {
		srows.Close()
		return doc, err
	}
	srows.Close()

	if err := tx.Commit(); err != nil {
		return doc, err
	}
	return doc, nil
}
