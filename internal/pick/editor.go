package pick

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/jstevewhite/snp/internal/store"
)

// Input is the write payload for creating or replacing a snippet. It
// mirrors store.SnippetInput and the API's snippetReq. A replace is a full
// replace, so an editor must send every field back, not just the edited
// ones.
type Input struct {
	Title         string
	Body          string
	Language      string
	Notes         string
	FolderID      *string
	Tags          []string
	IsSensitive   bool
	UsesVariables bool
	Pinned        bool
	VarDefaults   map[string]string
}

// Folder is one live folder. The tree is rebuilt from ParentID; neither
// the store nor the API carries a path.
type Folder struct {
	ID       string
	ParentID *string
	Name     string
}

// TagCount is a tag and how many live snippets carry it.
type TagCount struct {
	Name  string
	Count int
}

// Editor is the read and write surface the editor needs. Library stays the
// read-only surface the picker uses.
type Editor interface {
	Library
	Get(ctx context.Context, id string) (Snippet, error)
	Folders(ctx context.Context) ([]Folder, error)
	Tags(ctx context.Context) ([]TagCount, error)
	Create(ctx context.Context, in Input) (Snippet, error)
	Update(ctx context.Context, id string, in Input) (Snippet, error)
}

var (
	_ Editor  = Local{}
	_ Editor  = HTTP{}
	_ Library = Local{}
	_ Library = HTTP{}
)

func (in Input) snippetInput() store.SnippetInput {
	return store.SnippetInput{
		Title:         in.Title,
		Body:          in.Body,
		Language:      in.Language,
		Notes:         in.Notes,
		FolderID:      in.FolderID,
		Tags:          in.Tags,
		IsSensitive:   in.IsSensitive,
		UsesVariables: in.UsesVariables,
		Pinned:        in.Pinned,
		VarDefaults:   in.VarDefaults,
	}
}

func (l Local) Folders(ctx context.Context) ([]Folder, error) {
	rows, err := l.Store.ListFolders()
	if err != nil {
		return nil, err
	}
	out := make([]Folder, len(rows))
	for i, f := range rows {
		out[i] = Folder{ID: f.ID, ParentID: f.ParentID, Name: f.Name}
	}
	return out, nil
}

func (l Local) Tags(ctx context.Context) ([]TagCount, error) {
	rows, err := l.Store.ListTags()
	if err != nil {
		return nil, err
	}
	out := make([]TagCount, len(rows))
	for i, t := range rows {
		out[i] = TagCount{Name: t.Name, Count: t.Count}
	}
	return out, nil
}

func (l Local) Create(ctx context.Context, in Input) (Snippet, error) {
	row, err := l.Store.CreateSnippet(in.snippetInput())
	if err != nil {
		return Snippet{}, err
	}
	return fromOut(row), nil
}

func (l Local) Update(ctx context.Context, id string, in Input) (Snippet, error) {
	row, err := l.Store.ReplaceSnippet(id, in.snippetInput())
	if err != nil {
		return Snippet{}, err
	}
	return fromOut(row), nil
}

// snippetReq mirrors the server's request body for POST and PUT
// /api/snippets (internal/server/handlers.go). The endpoint is a full
// replace, so every field is sent; a field added server-side but not
// here is silently dropped by a full-replace PUT — change both together.
type snippetReq struct {
	Title         string            `json:"title"`
	Body          string            `json:"body"`
	Language      string            `json:"language"`
	Notes         string            `json:"notes"`
	FolderID      *string           `json:"folder_id"`
	Tags          []string          `json:"tags"`
	IsSensitive   bool              `json:"is_sensitive"`
	UsesVariables bool              `json:"uses_variables"`
	Pinned        bool              `json:"pinned"`
	VarDefaults   map[string]string `json:"var_defaults"`
}

func (in Input) req() snippetReq {
	return snippetReq{
		Title:         in.Title,
		Body:          in.Body,
		Language:      in.Language,
		Notes:         in.Notes,
		FolderID:      in.FolderID,
		Tags:          in.Tags,
		IsSensitive:   in.IsSensitive,
		UsesVariables: in.UsesVariables,
		Pinned:        in.Pinned,
		VarDefaults:   in.VarDefaults,
	}
}

func (h HTTP) Folders(ctx context.Context) ([]Folder, error) {
	body, err := h.get(ctx, "/api/folders", "")
	if err != nil {
		return nil, err
	}
	var rows []store.Folder
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	out := make([]Folder, len(rows))
	for i, f := range rows {
		out[i] = Folder{ID: f.ID, ParentID: f.ParentID, Name: f.Name}
	}
	return out, nil
}

func (h HTTP) Tags(ctx context.Context) ([]TagCount, error) {
	body, err := h.get(ctx, "/api/tags", "")
	if err != nil {
		return nil, err
	}
	var rows []store.TagCount
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	out := make([]TagCount, len(rows))
	for i, t := range rows {
		out[i] = TagCount{Name: t.Name, Count: t.Count}
	}
	return out, nil
}

func (h HTTP) Create(ctx context.Context, in Input) (Snippet, error) {
	return h.send(ctx, http.MethodPost, "/api/snippets", in.req(), http.StatusCreated)
}

func (h HTTP) Update(ctx context.Context, id string, in Input) (Snippet, error) {
	return h.send(ctx, http.MethodPut, "/api/snippets/"+url.PathEscape(id), in.req(), http.StatusOK)
}
