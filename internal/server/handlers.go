package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/jstevewhite/snp/internal/ai"
	"github.com/jstevewhite/snp/internal/buildinfo"
	"github.com/jstevewhite/snp/internal/portable"
	"github.com/jstevewhite/snp/internal/starter"
	"github.com/jstevewhite/snp/internal/store"
)

// apiMux routes the /api endpoints (spec §5).
func (s *Server) apiMux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/me", s.handleMe)
	mux.HandleFunc("GET /api/version", s.handleVersion)
	mux.HandleFunc("GET /api/snippets", s.handleListSnippets)
	mux.HandleFunc("POST /api/snippets", s.handleCreateSnippet)
	mux.HandleFunc("GET /api/snippets/{id}", s.handleGetSnippet)
	mux.HandleFunc("PUT /api/snippets/{id}", s.handleReplaceSnippet)
	mux.HandleFunc("DELETE /api/snippets/{id}", s.handleDeleteSnippet)
	mux.HandleFunc("GET /api/snippets/{id}/raw", s.handleRawSnippet)
	mux.HandleFunc("GET /api/trash", s.handleTrash)
	mux.HandleFunc("POST /api/snippets/{id}/restore", s.handleRestoreSnippet)
	mux.HandleFunc("GET /api/snippets/{id}/revisions", s.handleRevisions)
	mux.HandleFunc("GET /api/snippets/{id}/revisions/{revision}", s.handleRevision)
	mux.HandleFunc("POST /api/snippets/{id}/revisions/{revision}/restore", s.handleRestoreRevision)
	mux.HandleFunc("GET /api/folders", s.handleListFolders)
	mux.HandleFunc("POST /api/folders", s.handleCreateFolder)
	mux.HandleFunc("PUT /api/folders/{id}", s.handleUpdateFolder)
	mux.HandleFunc("DELETE /api/folders/{id}", s.handleDeleteFolder)
	mux.HandleFunc("GET /api/tags", s.handleListTags)
	mux.HandleFunc("GET /api/sync", s.handleSync)
	mux.HandleFunc("GET /api/export", s.handleExport)
	mux.HandleFunc("POST /api/export", s.handleExport)
	mux.HandleFunc("POST /api/decrypt-import", s.handleDecryptImport)
	mux.HandleFunc("POST /api/import", s.handleImport)
	mux.HandleFunc("POST /api/backup", s.handleBackup)
	mux.HandleFunc("POST /api/seed", s.handleSeed)
	mux.HandleFunc("GET /api/ai/status", s.handleAIStatus)
	mux.HandleFunc("POST /api/ai/generate", s.handleAIGenerate)
	mux.HandleFunc("POST /api/ai/tags", s.handleAISuggestTags)
	mux.HandleFunc("POST /api/ai/explain", s.handleAIExplain)
	return mux
}

// meOut is the /api/me response (spec §3).
type meOut struct {
	Login       string `json:"login"`
	DisplayName string `json:"display_name"`
}

// handleMe returns the caller's identity.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	id := identityFrom(r.Context())
	writeJSON(w, http.StatusOK, meOut{Login: id.Login, DisplayName: id.DisplayName})
}

// versionOut is the /api/version response. Unlike /api/me it carries no
// identity: the version is a property of the build, and the SPA shows it
// in the header for both the browser and the desktop window.
type versionOut struct {
	Version string `json:"version"`
}

// handleVersion reports the release version stamped into this binary
// (internal/buildinfo), or "dev" for a plain `go build`.
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, versionOut{Version: buildinfo.String()})
}

// snippetReq is the JSON body for POST/PUT /api/snippets. The id and
// timestamps are server-assigned and ignored on input.
type snippetReq struct {
	Title         string   `json:"title"`
	Body          string   `json:"body"`
	Language      string   `json:"language"`
	Notes         string   `json:"notes"`
	FolderID      *string  `json:"folder_id"`
	Tags          []string `json:"tags"`
	IsSensitive   bool     `json:"is_sensitive"`
	UsesVariables bool     `json:"uses_variables"`
	// Pinned is the favorite flag (spec §4); omitted decodes as false.
	Pinned bool `json:"pinned"`
	// VarDefaults carries per-variable default values for template
	// variables (spec §4); omitted decodes to an empty map.
	VarDefaults map[string]string `json:"var_defaults"`
}

func (q snippetReq) input() store.SnippetInput {
	return store.SnippetInput{
		Title:         q.Title,
		Body:          q.Body,
		Language:      q.Language,
		Notes:         q.Notes,
		FolderID:      q.FolderID,
		Tags:          q.Tags,
		IsSensitive:   q.IsSensitive,
		UsesVariables: q.UsesVariables,
		Pinned:        q.Pinned,
		VarDefaults:   q.VarDefaults,
	}
}

// handleListSnippets searches/lists live snippets (spec §5).
func (s *Server) handleListSnippets(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var folder *string
	if f := q.Get("folder"); f != "" {
		folder = &f
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	f := store.ListFilter{
		Q:      q.Get("q"),
		Tags:   q["tag"],
		Langs:  q["lang"],
		Folder: folder,
		Limit:  limit,
		Offset: offset,
	}
	out, err := s.store.ListSnippets(f)
	if err != nil {
		s.handleStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handleCreateSnippet creates a snippet; the server assigns id and
// timestamps (spec §5).
func (s *Server) handleCreateSnippet(w http.ResponseWriter, r *http.Request) {
	var req snippetReq
	if err := decodeJSON(r, &req); err != nil {
		s.handleBodyErr(w, err)
		return
	}
	out, err := s.store.CreateSnippet(req.input())
	if err != nil {
		s.handleStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

// handleGetSnippet returns one live snippet with its body decrypted.
func (s *Server) handleGetSnippet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	out, err := s.store.GetSnippet(r.PathValue("id"))
	if err != nil {
		s.handleStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handleReplaceSnippet fully replaces a live snippet, preserving
// created_at.
func (s *Server) handleReplaceSnippet(w http.ResponseWriter, r *http.Request) {
	var req snippetReq
	if err := decodeJSON(r, &req); err != nil {
		s.handleBodyErr(w, err)
		return
	}
	out, err := s.store.ReplaceSnippet(r.PathValue("id"), req.input())
	if err != nil {
		s.handleStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handleDeleteSnippet soft-deletes a live snippet.
func (s *Server) handleDeleteSnippet(w http.ResponseWriter, r *http.Request) {
	if err := s.store.SoftDeleteSnippet(r.PathValue("id")); err != nil {
		s.handleStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRawSnippet returns the decrypted body as text/plain, verbatim
// (no trailing newline) — for `curl | sh` (spec §5).
func (s *Server) handleRawSnippet(w http.ResponseWriter, r *http.Request) {
	body, err := s.store.RawBody(r.PathValue("id"))
	if err != nil {
		s.handleStoreErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, body)
}

// handleListFolders returns the full folder tree as a flat list with
// parent_id (spec §5).
func (s *Server) handleListFolders(w http.ResponseWriter, r *http.Request) {
	out, err := s.store.ListFolders()
	if err != nil {
		s.handleStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// folderReq is the JSON body for POST /api/folders.
type folderReq struct {
	Name     string  `json:"name"`
	ParentID *string `json:"parent_id"`
}

// handleCreateFolder creates a folder; 409 on a sibling name collision.
func (s *Server) handleCreateFolder(w http.ResponseWriter, r *http.Request) {
	var req folderReq
	if err := decodeJSON(r, &req); err != nil {
		s.handleBodyErr(w, err)
		return
	}
	out, err := s.store.CreateFolder(req.Name, req.ParentID)
	if err != nil {
		s.handleStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

// folderUpdateReq is the JSON body for PUT /api/folders/{id}. Fields are
// pointers so an omitted field means "no change".
type folderUpdateReq struct {
	Name     *string `json:"name"`
	ParentID *string `json:"parent_id"`
}

// handleUpdateFolder renames and/or moves a folder; 409 on a name
// collision, 400 if the move would create a cycle.
func (s *Server) handleUpdateFolder(w http.ResponseWriter, r *http.Request) {
	var req folderUpdateReq
	if err := decodeJSON(r, &req); err != nil {
		s.handleBodyErr(w, err)
		return
	}
	out, err := s.store.UpdateFolder(r.PathValue("id"), req.Name, req.ParentID)
	if err != nil {
		s.handleStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handleDeleteFolder soft-deletes a folder; refused with 409 while it has
// live children or snippets.
func (s *Server) handleDeleteFolder(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteFolder(r.PathValue("id")); err != nil {
		s.handleStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleListTags lists live tags with counts, most-used first.
func (s *Server) handleListTags(w http.ResponseWriter, r *http.Request) {
	out, err := s.store.ListTags()
	if err != nil {
		s.handleStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handleSync returns folders and snippets updated or deleted after
// ?since, plus server_time (spec §5).
func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	out, err := s.store.SyncSince(r.URL.Query().Get("since"))
	if err != nil {
		s.handleStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handleExport returns the full export document, bodies decrypted.
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	options, ok := s.downloadOptions(w, r)
	if !ok {
		return
	}
	if options.Encrypt {
		if !s.beginCrypto(w) {
			return
		}
		defer s.cryptoMu.Unlock()
	}
	out, err := s.store.Export()
	if err != nil {
		s.handleStoreErr(w, err)
		return
	}
	if !options.Encrypt {
		writeJSON(w, http.StatusOK, out)
		return
	}
	plain, err := json.Marshal(out)
	if err != nil {
		s.handleStoreErr(w, err)
		return
	}
	var encrypted bytes.Buffer
	if err := portable.Encrypt(&encrypted, bytes.NewReader(plain), options.Password); err != nil {
		s.handleStoreErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="snp-export.json.age"`)
	w.Write(encrypted.Bytes())
}

// handleImport applies an import document; ?mode=merge (default) or
// replace.
func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = "merge"
	}
	var doc store.ImportDoc
	if err := decodeJSON(r, &doc); err != nil {
		s.handleBodyErr(w, err)
		return
	}
	if doc.Snippets == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "snippets must be an explicit JSON array"})
		return
	}
	var out store.ImportResult
	var err error
	if r.URL.Query().Get("preview") == "1" {
		out, err = s.store.PreviewImport(doc, mode)
	} else {
		out, err = s.store.Import(doc, mode)
	}
	if err != nil {
		s.handleStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handleSeed applies the bundled starter pack (spec §5). The pack is only
// written on this explicit request: import's merge mode clears deleted_at,
// so seeding automatically would resurrect snippets the user deleted.
func (s *Server) handleSeed(w http.ResponseWriter, r *http.Request) {
	doc, err := starter.Pack()
	if err != nil {
		// Unreachable in a built binary (the pack is embedded); log and
		// keep the 5xx detail server-side.
		s.log.Error("starter pack unreadable", "err", err)
		writeJSON(w, http.StatusInternalServerError,
			map[string]string{"error": "internal error"})
		return
	}
	out, err := s.store.Import(doc, "merge")
	if err != nil {
		s.handleStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// aiStatusOut is the /api/ai/status response (spec §13). The endpoint
// and key are never exposed; only whether the feature is configured
// and which model would be used.
type aiStatusOut struct {
	Enabled bool   `json:"enabled"`
	Model   string `json:"model"`
}

// handleAIStatus reports whether AI snippet generation is configured.
func (s *Server) handleAIStatus(w http.ResponseWriter, r *http.Request) {
	if s.ai == nil {
		writeJSON(w, http.StatusOK, aiStatusOut{Enabled: false})
		return
	}
	writeJSON(w, http.StatusOK, aiStatusOut{Enabled: true, Model: s.ai.Model})
}

// aiGenerateReq is the body of POST /api/ai/generate.
type aiGenerateReq struct {
	Prompt   string `json:"prompt"`
	Language string `json:"language"`
	// Kind selects a single command (the default when empty), a
	// multi-line script, or a function definition (spec §13).
	Kind string `json:"kind"`
}

// aiGenerateOut mirrors what the snippet form needs (spec §13): the
// bare snippet in Body and its plain-text explanation in Notes.
type aiGenerateOut struct {
	Title         string `json:"title"`
	Language      string `json:"language"`
	Body          string `json:"body"`
	Notes         string `json:"notes"`
	UsesVariables bool   `json:"uses_variables"`
}

// handleAIGenerate runs one stateless generation request (no chat
// history) and returns snippet fields for the editor. The client
// reviews and saves through the normal snippet endpoints, so AI output
// never lands in the store un-reviewed.
func (s *Server) handleAIGenerate(w http.ResponseWriter, r *http.Request) {
	if s.ai == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "AI generation is not configured (set ai_key / SNP_AI_KEY)",
		})
		return
	}
	var req aiGenerateReq
	if err := decodeJSON(r, &req); err != nil {
		s.handleBodyErr(w, err)
		return
	}
	kind, err := validateAIReq(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	res, err := s.ai.Generate(r.Context(), ai.GenerateParams{
		Prompt:   req.Prompt,
		Language: req.Language,
		Kind:     kind,
	})
	if err != nil {
		s.logAIError(err)
		switch {
		case errors.Is(err, store.ErrNoKey): // unreachable; kept for symmetry
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		default:
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "AI provider error"})
		}
		return
	}
	writeJSON(w, http.StatusOK, aiGenerateOut{
		Title:         res.Title,
		Language:      res.Language,
		Body:          res.Body,
		Notes:         res.Notes,
		UsesVariables: strings.Contains(res.Body, "{{"),
	})
}

// validateAIReq checks a generate request and normalizes its kind. An
// unrecognized kind is rejected rather than silently treated as a
// command (spec §13).
func validateAIReq(req aiGenerateReq) (ai.Kind, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return "", errors.New("prompt is required")
	}
	if len(req.Prompt) > 4000 {
		return "", errors.New("prompt is too long (max 4000 characters)")
	}
	kind, ok := ai.ParseKind(req.Kind)
	if !ok {
		return "", errors.New("kind must be command, script, or function")
	}
	return kind, nil
}

// logAIError records generation failures with detail (never the prompt,
// the key, or provider bodies) and maps them to a generic 502.
func (s *Server) logAIError(err error) {
	var ue *ai.UpstreamError
	if errors.As(err, &ue) {
		s.log.Warn("ai generate failed", "status", ue.Status)
		return
	}
	var oe *ai.OutputError
	if errors.As(err, &oe) {
		s.log.Warn("ai generate output unparseable", "reason", oe.Reason)
		return
	}
	s.log.Error("ai generate failed", "err", err)
}

// aiTagsReq is the body of POST /api/ai/tags.
type aiTagsReq struct {
	Body        string `json:"body"`
	Title       string `json:"title"`
	Language    string `json:"language"`
	IsSensitive *bool  `json:"is_sensitive"`
}

// allowAIBody requires the draft's sensitivity explicitly (spec §13).
// Older clients must fail closed rather than silently send sensitive bodies.
func allowAIBody(w http.ResponseWriter, sensitive *bool) bool {
	if sensitive == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "is_sensitive is required"})
		return false
	}
	if *sensitive {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "AI actions are unavailable for sensitive snippets"})
		return false
	}
	return true
}

// aiTagsOut lists the suggested tag names (validated against the store
// grammar, de-duplicated, capped at 3).
type aiTagsOut struct {
	Tags []string `json:"tags"`
}

// handleAISuggestTags returns 2-3 relevant tags for the given snippet
// body, reusing the user's existing tag vocabulary and allowing new
// ones (spec §13). Model output is filtered to names the store would
// accept, so a suggestion can never poison a later save.
func (s *Server) handleAISuggestTags(w http.ResponseWriter, r *http.Request) {
	if s.ai == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "AI generation is not configured (set ai_key / SNP_AI_KEY)",
		})
		return
	}
	var req aiTagsReq
	if err := decodeJSON(r, &req); err != nil {
		s.handleBodyErr(w, err)
		return
	}
	if !allowAIBody(w, req.IsSensitive) {
		return
	}
	if strings.TrimSpace(req.Body) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body is required"})
		return
	}
	counts, err := s.store.ListTags()
	if err != nil {
		s.handleStoreErr(w, err)
		return
	}
	existing := make([]string, 0, len(counts))
	for _, tc := range counts {
		existing = append(existing, tc.Name)
	}
	res, err := s.ai.SuggestTags(r.Context(), ai.TagSuggestParams{
		Body:     req.Body,
		Title:    req.Title,
		Language: req.Language,
		Existing: existing,
	})
	if err != nil {
		s.logAIError(err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "AI provider error"})
		return
	}
	out := []string{}
	seen := map[string]bool{}
	for _, t := range res {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || seen[t] || !store.ValidTagName(t) {
			continue
		}
		seen[t] = true
		out = append(out, t)
		if len(out) == 3 {
			break
		}
	}
	writeJSON(w, http.StatusOK, aiTagsOut{Tags: out})
}

// aiExplainReq is the body of POST /api/ai/explain.
type aiExplainReq struct {
	Body        string `json:"body"`
	IsSensitive *bool  `json:"is_sensitive"`
}

// aiExplainOut carries the plain-text explanation for the Notes field.
type aiExplainOut struct {
	Notes string `json:"notes"`
}

// handleAIExplain returns a plain-text explanation of a command
// (what it does, gotchas, important info) for the snippet's Notes
// field (spec §13). One-shot; there is no hard max_tokens cap — the
// prompt asks for a reply under 500 tokens so reasoning is not
// truncated.
func (s *Server) handleAIExplain(w http.ResponseWriter, r *http.Request) {
	if s.ai == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "AI generation is not configured (set ai_key / SNP_AI_KEY)",
		})
		return
	}
	var req aiExplainReq
	if err := decodeJSON(r, &req); err != nil {
		s.handleBodyErr(w, err)
		return
	}
	if !allowAIBody(w, req.IsSensitive) {
		return
	}
	if strings.TrimSpace(req.Body) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body is required"})
		return
	}
	notes, err := s.ai.Explain(r.Context(), req.Body)
	if err != nil {
		s.logAIError(err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "AI provider error"})
		return
	}
	writeJSON(w, http.StatusOK, aiExplainOut{Notes: notes})
}
