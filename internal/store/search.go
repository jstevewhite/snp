package store

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// ListFilter drives ListSnippets. Q uses the spec §5 query syntax
// (tag:x / lang:x tokens mixed with FTS terms).
type ListFilter struct {
	Q      string
	Tags   []string
	Langs  []string
	Folder *string
	Limit  int
	Offset int
}

var (
	tagFilterRe  = regexp.MustCompile(`^tag:(.+)$`)
	langFilterRe = regexp.MustCompile(`^lang:(.+)$`)
)

// ParseQuery splits q into search terms, tag filters, and language
// filters (spec §5). Terms come back individually so the caller can turn
// each one into a prefix query.
func ParseQuery(q string) (terms, tags, langs []string) {
	for _, tok := range strings.Fields(q) {
		if m := tagFilterRe.FindStringSubmatch(tok); m != nil {
			tags = append(tags, m[1])
			continue
		}
		if m := langFilterRe.FindStringSubmatch(tok); m != nil {
			langs = append(langs, m[1])
			continue
		}
		terms = append(terms, tok)
	}
	return terms, tags, langs
}

// prefixExpr renders terms as an FTS5 MATCH expression in which every term
// is a prefix query: `"term"*`. FTS5 ANDs adjacent terms, so a snippet
// matches only when every term prefixes a token somewhere in the indexed
// columns — which is what the offline engine does too (spec §6).
//
// Quoting the term (doubling any embedded quote) means FTS5 syntax in user
// input is literal text, not operators: `AND`, `OR`, `NOT`, `NEAR` and
// column filters like `title:x` match as words. That is deliberate — the
// offline index has no operators, so treating them literally is what keeps
// the two engines agreeing.
//
// A term with no letters or digits cannot prefix any token, so it is
// dropped rather than emitted as an empty phrase, which FTS5 rejects.
func prefixExpr(terms []string) string {
	parts := make([]string, 0, len(terms))
	for _, term := range terms {
		if !strings.ContainsFunc(term, isWordRune) {
			continue
		}
		parts = append(parts, `"`+strings.ReplaceAll(term, `"`, `""`)+`"*`)
	}
	return strings.Join(parts, " ")
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// ListSnippets searches/lists live snippets (spec §5). Sensitive
// bodies are never returned in this context.
func (s *Store) ListSnippets(f ListFilter) ([]SnippetOut, error) {
	ctx := context.Background()
	pqTerms, pqTags, pqLangs := ParseQuery(f.Q)
	ftsExpr := prefixExpr(pqTerms)
	tags := dedupe(append(append([]string{}, f.Tags...), pqTags...))
	langs := dedupe(append(append([]string{}, f.Langs...), pqLangs...))
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}

	run := func(ftsExpr string) ([]SnippetOut, error) {
		var b strings.Builder
		b.WriteString(`SELECT s.id, s.title, s.body, s.language, s.notes, s.folder_id,
			s.is_sensitive, s.uses_variables, s.pinned, s.var_defaults, s.var_defaults_enc,
			s.created_at, s.updated_at
			FROM snippets s`)
		var args []any
		if ftsExpr != "" {
			b.WriteString(` JOIN snippets_fts ON snippets_fts.rowid = s.rowid AND snippets_fts MATCH ?`)
			args = append(args, ftsExpr)
		}
		b.WriteString(` WHERE s.deleted_at IS NULL`)
		if f.Folder != nil {
			b.WriteString(` AND s.folder_id = ?`)
			args = append(args, *f.Folder)
		}
		if len(langs) > 0 {
			b.WriteString(` AND s.language IN (` + placeholders(len(langs)) + `)`)
			for _, l := range langs {
				args = append(args, l)
			}
		}
		for _, t := range tags {
			b.WriteString(` AND EXISTS (SELECT 1 FROM snippet_tags st
				JOIN tags tg ON tg.id = st.tag_id
				WHERE st.snippet_id = s.id AND tg.name = ?)`)
			args = append(args, t)
		}
		if ftsExpr != "" {
			b.WriteString(` ORDER BY bm25(snippets_fts, 10.0, 1.0, 1.0, 1.0)`)
		} else {
			b.WriteString(` ORDER BY s.updated_at DESC, s.id DESC`)
		}
		b.WriteString(` LIMIT ? OFFSET ?`)
		args = append(args, limit, offset)

		rows, err := s.db.QueryContext(ctx, b.String(), args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		out := []SnippetOut{}
		var ids []string
		for rows.Next() {
			var sn SnippetOut
			var body []byte
			var folder sql.NullString
			var sens, uvars, pinned int
			var vdPlain string
			var vdEnc sql.Null[[]byte]
			if err := rows.Scan(&sn.ID, &sn.Title, &body, &sn.Language, &sn.Notes,
				&folder, &sens, &uvars, &pinned, &vdPlain, &vdEnc, &sn.CreatedAt, &sn.UpdatedAt); err != nil {
				return nil, err
			}
			if folder.Valid {
				sn.FolderID = &folder.String
			}
			sn.IsSensitive = sens != 0
			sn.UsesVariables = uvars != 0
			sn.Pinned = pinned != 0
			if !sn.IsSensitive {
				b := string(body)
				sn.Body = &b
				vd, err := s.loadVarDefaults(sn.ID, vdPlain, nil, false)
				if err != nil {
					return nil, err
				}
				sn.VarDefaults = vd
			}
			out = append(out, sn)
			ids = append(ids, sn.ID)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		tagMap, err := s.tagsFor(ctx, ids)
		if err != nil {
			return nil, err
		}
		for i := range out {
			out[i].Tags = tagMap[out[i].ID]
		}
		return out, nil
	}

	out, err := run(ftsExpr)
	if err != nil && ftsExpr != "" {
		// Safety net: prefixExpr is quoted, so FTS5 should accept it, but
		// the tokenizer remains the authority on what a valid expression
		// is. Retry the terms as one quoted phrase; if even that fails,
		// surface ErrFTS (a 400 at the API layer, spec §5).
		quoted := `"` + strings.ReplaceAll(strings.Join(pqTerms, " "), `"`, `""`) + `"`
		out, err = run(quoted)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrFTS, err)
		}
	}
	return out, err
}
