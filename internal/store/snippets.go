package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/oklog/ulid"
)

// SnippetInput is the payload for creating or replacing a snippet.
// Body is always plaintext; the store encrypts it when IsSensitive.
type SnippetInput struct {
	Title       string
	Body        string
	Language    string
	Notes       string
	FolderID    *string
	Tags        []string
	IsSensitive bool
	// UsesVariables marks the body as a template (spec §4). The server
	// treats the body as opaque text; this only drives frontend behavior.
	UsesVariables bool
	// Pinned marks the snippet as a favorite (spec §4), surfaced in the
	// app's Favorites list. A pin is not sensitive content, so it is stored
	// plainly for every row and is never FTS-indexed.
	Pinned bool
	// VarDefaults holds per-variable default values for the body's
	// template variables (spec §4). The server carries the map opaquely;
	// pruning keys to the body's current variables is the client's job.
	VarDefaults map[string]string
}

// SnippetOut is the API representation of a snippet. Body is nil for
// sensitive snippets in list/sync contexts and always populated for
// single-snippet reads (spec §5).
type SnippetOut struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Body        *string  `json:"body"`
	Language    string   `json:"language"`
	Notes       string   `json:"notes"`
	FolderID    *string  `json:"folder_id"`
	Tags        []string `json:"tags"`
	IsSensitive bool     `json:"is_sensitive"`
	// UsesVariables marks the body as a template (spec §4).
	UsesVariables bool `json:"uses_variables"`
	// Pinned marks the snippet as a favorite (spec §4). Always present in
	// JSON output; an older client simply ignores it.
	Pinned bool `json:"pinned"`
	// VarDefaults holds per-variable default values (spec §4). It is nil
	// (JSON null) for sensitive snippets in list/sync contexts, mirroring
	// how their body is hidden there (spec §5).
	VarDefaults map[string]string `json:"var_defaults"`
	CreatedAt   string            `json:"created_at"`
	UpdatedAt   string            `json:"updated_at"`
}

var tagRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// ValidTagName reports whether name matches the tag grammar (spec §4):
// lowercase, starting with a letter or digit, then letters, digits, or
// hyphens. The store applies it on every tag write; the AI tag
// suggestion endpoint reuses it to filter model output.
func ValidTagName(name string) bool { return tagRe.MatchString(name) }

func validateSnippetInput(in SnippetInput) error {
	if strings.TrimSpace(in.Title) == "" {
		return fmt.Errorf("%w: title required", ErrInvalid)
	}
	for _, raw := range in.Tags {
		if !tagRe.MatchString(strings.ToLower(strings.TrimSpace(raw))) {
			return fmt.Errorf("%w: %q", ErrInvalidTag, raw)
		}
	}
	return nil
}

func bodyForIndex(in SnippetInput) string {
	if in.IsSensitive {
		return ""
	}
	return in.Body
}

func (s *Store) storeBody(id, plaintext string, sensitive bool) ([]byte, error) {
	if !sensitive {
		return []byte(plaintext), nil
	}
	k, err := s.requireKey()
	if err != nil {
		return nil, err
	}
	return k.Seal(id, []byte(plaintext))
}

// encodeVarDefaults marshals the map for storage; an empty map encodes
// as {} so the JSON is never null.
func encodeVarDefaults(m map[string]string) (string, error) {
	if m == nil {
		m = map[string]string{}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// decodeVarDefaults parses a var_defaults column value or a decrypted
// payload. ” (legacy rows, and sensitive rows with no defaults) and
// null decode to an empty map.
func decodeVarDefaults(text string) (map[string]string, error) {
	m := map[string]string{}
	if text != "" {
		if err := json.Unmarshal([]byte(text), &m); err != nil {
			return nil, err
		}
		if m == nil {
			m = map[string]string{}
		}
	}
	return m, nil
}

// storeVarDefaults returns the (plain, encrypted) column values for the
// var_defaults columns (spec §4 "Encryption"): the JSON text for
// non-sensitive rows; ” plus a map sealed under the snippet id for
// sensitive rows, NULL when there is nothing to seal.
func (s *Store) storeVarDefaults(id string, defaults map[string]string, sensitive bool) (string, []byte, error) {
	jsonText, err := encodeVarDefaults(defaults)
	if err != nil {
		return "", nil, err
	}
	if !sensitive {
		return jsonText, nil, nil
	}
	if len(defaults) == 0 {
		return "", nil, nil
	}
	k, err := s.requireKey()
	if err != nil {
		return "", nil, err
	}
	enc, err := k.Seal(id, []byte(jsonText))
	if err != nil {
		return "", nil, err
	}
	return "", enc, nil
}

// loadVarDefaults reads the var_defaults columns back into a map.
// Sensitive rows are decrypted under the snippet id (spec §4).
func (s *Store) loadVarDefaults(id, plain string, enc []byte, sensitive bool) (map[string]string, error) {
	if sensitive {
		if len(enc) == 0 {
			return map[string]string{}, nil
		}
		k, err := s.requireKey()
		if err != nil {
			return nil, err
		}
		pt, err := k.Open(id, enc)
		if err != nil {
			return nil, err
		}
		return decodeVarDefaults(string(pt))
	}
	return decodeVarDefaults(plain)
}

// varDefaultsOut returns a non-nil map for API output ({} not null).
func varDefaultsOut(m map[string]string) map[string]string {
	if m == nil {
		m = map[string]string{}
	}
	return m
}

// CreateSnippet inserts a new snippet and returns it.
func (s *Store) CreateSnippet(in SnippetInput) (SnippetOut, error) {
	if err := validateSnippetInput(in); err != nil {
		return SnippetOut{}, err
	}
	ctx := context.Background()
	now := s.now()
	id := ulid.MustNew(ulid.Now(), rand.Reader).String()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SnippetOut{}, err
	}
	defer tx.Rollback()

	if in.FolderID != nil {
		if err := liveFolderTx(tx, *in.FolderID); err != nil {
			return SnippetOut{}, err
		}
	}
	body, err := s.storeBody(id, in.Body, in.IsSensitive)
	if err != nil {
		return SnippetOut{}, err
	}
	vdPlain, vdEnc, err := s.storeVarDefaults(id, in.VarDefaults, in.IsSensitive)
	if err != nil {
		return SnippetOut{}, err
	}
	bodyText := bodyForIndex(in)
	res, err := tx.ExecContext(ctx,
		`INSERT INTO snippets (id, title, body, body_text, language, notes, tags, folder_id,
		 is_sensitive, uses_variables, pinned, var_defaults, var_defaults_enc, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, '', ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, in.Title, body, bodyText, in.Language, in.Notes, in.FolderID, boolInt(in.IsSensitive),
		boolInt(in.UsesVariables), boolInt(in.Pinned), vdPlain, vdEnc, now, now)
	if err != nil {
		return SnippetOut{}, err
	}
	rowid, err := res.LastInsertId()
	if err != nil {
		return SnippetOut{}, err
	}
	tags, err := s.setTagsTx(ctx, tx, id, in.Tags)
	if err != nil {
		return SnippetOut{}, err
	}
	// The rowid is new and not yet in the FTS index, so insert only.
	if err := s.insertFTSTx(ctx, tx, rowid, in.Title, in.Notes, bodyText, strings.Join(tags, " ")); err != nil {
		return SnippetOut{}, err
	}
	if err := tx.Commit(); err != nil {
		return SnippetOut{}, err
	}

	out := SnippetOut{
		ID:            id,
		Title:         in.Title,
		Language:      in.Language,
		Notes:         in.Notes,
		FolderID:      in.FolderID,
		Tags:          tags,
		IsSensitive:   in.IsSensitive,
		UsesVariables: in.UsesVariables,
		Pinned:        in.Pinned,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if !in.IsSensitive {
		out.Body = &in.Body
		out.VarDefaults = varDefaultsOut(in.VarDefaults)
	}
	return out, nil
}

// ReplaceSnippet fully replaces a live snippet, preserving created_at.
func (s *Store) ReplaceSnippet(id string, in SnippetInput) (SnippetOut, error) {
	if err := validateSnippetInput(in); err != nil {
		return SnippetOut{}, err
	}
	ctx := context.Background()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SnippetOut{}, err
	}
	defer tx.Rollback()

	out, err := s.replaceSnippetTx(ctx, tx, id, in)
	if err != nil {
		return SnippetOut{}, err
	}
	if err := tx.Commit(); err != nil {
		return SnippetOut{}, err
	}
	return out, nil
}

func (s *Store) replaceSnippetTx(ctx context.Context, tx *sql.Tx, id string, in SnippetInput) (SnippetOut, error) {
	var created string
	var rowid int64
	if err := tx.QueryRowContext(ctx,
		`SELECT created_at, rowid FROM snippets WHERE id = ? AND deleted_at IS NULL`, id).Scan(&created, &rowid); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SnippetOut{}, ErrNotFound
		}
		return SnippetOut{}, err
	}
	if in.FolderID != nil {
		if err := liveFolderTx(tx, *in.FolderID); err != nil {
			return SnippetOut{}, err
		}
	}
	if err := s.captureRevisionTx(ctx, tx, id, in); err != nil {
		return SnippetOut{}, err
	}
	now := s.now()
	body, err := s.storeBody(id, in.Body, in.IsSensitive)
	if err != nil {
		return SnippetOut{}, err
	}
	vdPlain, vdEnc, err := s.storeVarDefaults(id, in.VarDefaults, in.IsSensitive)
	if err != nil {
		return SnippetOut{}, err
	}
	// Remove the old FTS row while the content row still holds the old
	// values; an external-content FTS5 delete reads them from the content
	// table. The rowid is already indexed because the snippet exists.
	if err := s.deleteFTSTx(ctx, tx, rowid); err != nil {
		return SnippetOut{}, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE snippets SET title = ?, body = ?, body_text = ?, language = ?, notes = ?, folder_id = ?,
		 is_sensitive = ?, uses_variables = ?, pinned = ?, var_defaults = ?, var_defaults_enc = ?, updated_at = ? WHERE id = ?`,
		in.Title, body, bodyForIndex(in), in.Language, in.Notes, in.FolderID, boolInt(in.IsSensitive),
		boolInt(in.UsesVariables), boolInt(in.Pinned), vdPlain, vdEnc, now, id); err != nil {
		return SnippetOut{}, err
	}
	tags, err := s.setTagsTx(ctx, tx, id, in.Tags)
	if err != nil {
		return SnippetOut{}, err
	}
	if err := s.insertFTSTx(ctx, tx, rowid, in.Title, in.Notes, bodyForIndex(in), strings.Join(tags, " ")); err != nil {
		return SnippetOut{}, err
	}

	out := SnippetOut{
		ID:            id,
		Title:         in.Title,
		Language:      in.Language,
		Notes:         in.Notes,
		FolderID:      in.FolderID,
		Tags:          tags,
		IsSensitive:   in.IsSensitive,
		UsesVariables: in.UsesVariables,
		Pinned:        in.Pinned,
		CreatedAt:     created,
		UpdatedAt:     now,
	}
	if !in.IsSensitive {
		out.Body = &in.Body
		out.VarDefaults = varDefaultsOut(in.VarDefaults)
	}
	return out, nil
}

// GetSnippet returns a live snippet with its body decrypted.
func (s *Store) GetSnippet(id string) (SnippetOut, error) {
	return s.getSnippet(context.Background(), s.db, id, true)
}

// snippetReader permits consistent reads within a write transaction.
type snippetReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Store) getSnippet(ctx context.Context, q snippetReader, id string, live bool) (SnippetOut, error) {
	var (
		out     SnippetOut
		body    []byte
		folder  sql.NullString
		sens    int
		uvars   int
		pinned  int
		vdPlain string
		tagText string
		vdEnc   sql.Null[[]byte]
	)
	where := ""
	if live {
		where = " AND deleted_at IS NULL"
	}
	err := q.QueryRowContext(ctx,
		`SELECT id, title, body, language, notes, folder_id, is_sensitive, uses_variables, pinned,
		 var_defaults, var_defaults_enc, created_at, updated_at, tags
		 FROM snippets WHERE id = ?`+where, id).
		Scan(&out.ID, &out.Title, &body, &out.Language, &out.Notes, &folder, &sens, &uvars, &pinned,
			&vdPlain, &vdEnc, &out.CreatedAt, &out.UpdatedAt, &tagText)
	if errors.Is(err, sql.ErrNoRows) {
		return SnippetOut{}, ErrNotFound
	}
	if err != nil {
		return SnippetOut{}, err
	}
	if folder.Valid {
		out.FolderID = &folder.String
	}
	out.IsSensitive = sens != 0
	out.UsesVariables = uvars != 0
	out.Pinned = pinned != 0
	if out.IsSensitive {
		k, err := s.requireKey()
		if err != nil {
			return SnippetOut{}, err
		}
		pt, err := k.Open(id, body)
		if err != nil {
			return SnippetOut{}, err
		}
		plain := string(pt)
		out.Body = &plain
	} else {
		b := string(body)
		out.Body = &b
	}
	var encBytes []byte
	if vdEnc.Valid {
		encBytes = vdEnc.V
	}
	out.VarDefaults, err = s.loadVarDefaults(id, vdPlain, encBytes, out.IsSensitive)
	if err != nil {
		return SnippetOut{}, err
	}
	out.Tags = strings.Fields(tagText)
	if out.Tags == nil {
		out.Tags = []string{}
	}
	return out, nil
}

// RawBody returns the decrypted body of a live snippet.
func (s *Store) RawBody(id string) (string, error) {
	out, err := s.GetSnippet(id)
	if err != nil {
		return "", err
	}
	if out.Body == nil {
		return "", nil
	}
	return *out.Body, nil
}

// SoftDeleteSnippet marks a live snippet deleted. The FTS row stays
// until hard purge; searches filter on deleted_at.
func (s *Store) SoftDeleteSnippet(id string) error {
	ctx := context.Background()
	now := s.now()
	res, err := s.db.ExecContext(ctx,
		`UPDATE snippets SET deleted_at = ? WHERE id = ? AND deleted_at IS NULL`, now, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// setTagsTx replaces the snippet's tag set and mirrors the joined names
// into snippets.tags (required by the external-content FTS table).
func (s *Store) setTagsTx(ctx context.Context, tx *sql.Tx, snippetID string, names []string) ([]string, error) {
	if _, err := tx.ExecContext(ctx, `DELETE FROM snippet_tags WHERE snippet_id = ?`, snippetID); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(names))
	seen := map[string]bool{}
	for _, raw := range names {
		name := strings.ToLower(strings.TrimSpace(raw))
		if !tagRe.MatchString(name) {
			return nil, fmt.Errorf("%w: %q", ErrInvalidTag, raw)
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		var id int
		err := tx.QueryRowContext(ctx, `SELECT id FROM tags WHERE name = ?`, name).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			res, err := tx.ExecContext(ctx, `INSERT INTO tags (name) VALUES (?)`, name)
			if err != nil {
				return nil, err
			}
			id64, err := res.LastInsertId()
			if err != nil {
				return nil, err
			}
			id = int(id64)
		} else if err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO snippet_tags (snippet_id, tag_id) VALUES (?, ?)`, snippetID, id); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE snippets SET tags = ? WHERE id = ?`, strings.Join(out, " "), snippetID); err != nil {
		return nil, err
	}
	return out, nil
}

// deleteFTSTx removes a snippet's row from the FTS index.
//
// The rowid must already be present in the index. An external-content
// FTS5 DELETE on a rowid that was never inserted corrupts the index
// ("database disk image is malformed"), so only call this for rows that
// were previously indexed, and do it before the content row is updated:
// the delete reads the old values from the content table.
func (s *Store) deleteFTSTx(ctx context.Context, tx *sql.Tx, rowid int64) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM snippets_fts WHERE rowid = ?`, rowid)
	return err
}

// insertFTSTx adds a snippet's row to the FTS index. Sensitive bodies are
// indexed empty so they are never searchable (spec §4).
func (s *Store) insertFTSTx(ctx context.Context, tx *sql.Tx, rowid int64, title, notes, bodyForIndex, tagsJoined string) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO snippets_fts (rowid, title, notes, body_text, tags) VALUES (?, ?, ?, ?, ?)`,
		rowid, title, notes, bodyForIndex, tagsJoined)
	return err
}

// tagsFor loads the tag names for the given snippet ids.
func (s *Store) tagsFor(ctx context.Context, ids []string) (map[string][]string, error) {
	out := map[string][]string{}
	if len(ids) == 0 {
		return out, nil
	}
	q := `SELECT st.snippet_id, tg.name
		 FROM snippet_tags st
		 JOIN tags tg ON tg.id = st.tag_id
		 WHERE st.snippet_id IN (` + placeholders(len(ids)) + `)`
	rows, err := s.db.QueryContext(ctx, q, toAny(ids)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var sid, name string
		if err := rows.Scan(&sid, &name); err != nil {
			return nil, err
		}
		out[sid] = append(out[sid], name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, id := range ids {
		if _, ok := out[id]; !ok {
			out[id] = []string{}
		}
	}
	return out, nil
}

// liveFolderTx fails with ErrNotFound unless id is a live folder.
func liveFolderTx(tx *sql.Tx, id string) error {
	var n int
	if err := tx.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM folders WHERE id = ? AND deleted_at IS NULL`, id).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
