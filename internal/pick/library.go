// Package pick is the shell snippet picker: a terminal list and a
// variable form whose accepted text is written to stdout for a shell
// widget to drop into the line editor.
package pick

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jstevewhite/snp/internal/store"
	"github.com/jstevewhite/snp/internal/template"
)

// ErrCanceled means the picker closed without a command.
var ErrCanceled = errors.New("canceled")

// Limit is the most snippets one search returns. It matches the API cap.
const Limit = 200

// Snippet is one library row the picker can show or insert.
type Snippet struct {
	ID            string
	Title         string
	Body          string
	Language      string
	Notes         string
	Tags          []string
	Sensitive     bool
	UsesVariables bool
	VarDefaults   map[string]string
}

// IsTemplate reports whether accepting this row should open the variable
// form. A sensitive list row hides its body, so the stored flag counts;
// once the body is present, the placeholders themselves decide.
func (s Snippet) IsTemplate() bool {
	if s.Body != "" {
		return template.HasVars(s.Body)
	}
	return s.UsesVariables
}

// Library is the read surface the picker needs. Search matches the app's
// query language. Reveal fetches a sensitive body, which list results omit.
type Library interface {
	Search(ctx context.Context, q string) ([]Snippet, error)
	Reveal(ctx context.Context, id string) (Snippet, error)
}

func fromOut(o store.SnippetOut) Snippet {
	s := Snippet{
		ID:            o.ID,
		Title:         o.Title,
		Language:      o.Language,
		Notes:         o.Notes,
		Tags:          o.Tags,
		Sensitive:     o.IsSensitive,
		UsesVariables: o.UsesVariables,
		VarDefaults:   o.VarDefaults,
	}
	if o.Body != nil {
		s.Body = *o.Body
	}
	if s.Tags == nil {
		s.Tags = []string{}
	}
	return s
}

// Local reads the on-disk database. The store may be opened without a key;
// revealing a sensitive row then fails with the store's own error.
type Local struct{ Store *store.Store }

func (l Local) Search(ctx context.Context, q string) ([]Snippet, error) {
	rows, err := l.Store.ListSnippets(store.ListFilter{Q: q, Limit: Limit})
	if err != nil {
		return nil, err
	}
	return mapRows(rows), nil
}

func (l Local) Reveal(ctx context.Context, id string) (Snippet, error) {
	row, err := l.Store.GetSnippet(id)
	if err != nil {
		return Snippet{}, err
	}
	return fromOut(row), nil
}

// HTTP talks to a snp server. Tailnet calls send no credential: WhoIs
// authenticates the source address. A --dev-listen URL is the same client.
type HTTP struct {
	Base   *url.URL
	Client *http.Client
}

// NewHTTP parses a library base URL. Only http and https are accepted.
func NewHTTP(raw string) (HTTP, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return HTTP{}, fmt.Errorf("url must be an http or https address")
	}
	return HTTP{
		Base:   u,
		Client: &http.Client{Timeout: 15 * time.Second},
	}, nil
}

func (h HTTP) Search(ctx context.Context, q string) ([]Snippet, error) {
	query := url.Values{}
	query.Set("q", q)
	query.Set("limit", fmt.Sprintf("%d", Limit))
	body, err := h.get(ctx, "/api/snippets", query.Encode())
	if err != nil {
		return nil, err
	}
	var rows []store.SnippetOut
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	return mapRows(rows), nil
}

func (h HTTP) Reveal(ctx context.Context, id string) (Snippet, error) {
	body, err := h.get(ctx, "/api/snippets/"+url.PathEscape(id), "")
	if err != nil {
		return Snippet{}, err
	}
	var row store.SnippetOut
	if err := json.Unmarshal(body, &row); err != nil {
		return Snippet{}, fmt.Errorf("library: %w", err)
	}
	return fromOut(row), nil
}

func (h HTTP) get(ctx context.Context, path, rawQuery string) ([]byte, error) {
	u := h.Base.JoinPath(path)
	u.RawQuery = rawQuery
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := h.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		var api struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &api) == nil && api.Error != "" {
			return nil, fmt.Errorf("library: %s", api.Error)
		}
		return nil, fmt.Errorf("library: HTTP %d", resp.StatusCode)
	}
	return data, nil
}

func mapRows(rows []store.SnippetOut) []Snippet {
	out := make([]Snippet, len(rows))
	for i, row := range rows {
		out[i] = fromOut(row)
	}
	return out
}

// CommandText is what stdout receives. A sensitive command gains one
// leading space when it has none, so zsh history can skip it under
// HIST_IGNORE_SPACE. The shell still runs it the same way.
func CommandText(s Snippet, body string) string {
	if !s.Sensitive || body == "" {
		return body
	}
	r, _ := utf8.DecodeRuneInString(body)
	if unicode.IsSpace(r) {
		return body
	}
	return " " + body
}
