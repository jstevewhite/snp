package ask

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jstevewhite/snp/internal/ai"
	"github.com/jstevewhite/snp/internal/store"
)

// providerClient returns an ai.Client pointed at an httptest chat
// completions endpoint, the internal/ai test pattern. The handler sees
// each request's messages; reply feeds the assistant content.
func providerClient(t *testing.T, reply func(payload chatRequest) string) (*ai.Client, *[]chatRequest) {
	t.Helper()
	var seen []chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode provider request: %v", err)
		}
		seen = append(seen, req)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(chatResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{{Message: struct {
				Content string `json:"content"`
			}{Content: reply(req)}}},
		})
	}))
	t.Cleanup(srv.Close)
	return &ai.Client{Endpoint: srv.URL, Model: "test-model", APIKey: "k", HTTP: srv.Client()}, &seen
}

// chatRequest/chatResponse mirror the provider wire format enough for
// the test fake (internal/ai owns the real ones).
type chatRequest struct {
	Model    string          `json:"model"`
	Messages []chatMessageF `json:"messages"`
}

type chatMessageF struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func TestErrorTextsMatchServer(t *testing.T) {
	// These literals are internal/server/handlers.go's own texts
	// (the 503 and the 403). The tests pin both transports to them;
	// change both together.
	if ErrUnavailable.Error() != "AI generation is not configured (set ai_key / SNP_AI_KEY)" {
		t.Fatalf("unavailable text = %q", ErrUnavailable.Error())
	}
	if ErrSensitive.Error() != "AI actions are unavailable for sensitive snippets" {
		t.Fatalf("sensitive text = %q", ErrSensitive.Error())
	}
}

func TestLocalStatus(t *testing.T) {
	if st, err := (Local{}).Status(context.Background()); err != nil || st.Enabled || st.Model != "" {
		t.Fatalf("disabled status = %+v, %v", st, err)
	}
	c, _ := providerClient(t, func(chatRequest) string { return "" })
	st, err := Local{Client: c}.Status(context.Background())
	if err != nil || !st.Enabled || st.Model != "test-model" {
		t.Fatalf("status = %+v, %v", st, err)
	}
}

func TestLocalGeneratePolicies(t *testing.T) {
	ctx := context.Background()
	// Unconfigured first, exactly like the server's 503-before-anything.
	if _, err := (Local{}).Generate(ctx, "p", ai.KindCommand, ""); err != ErrUnavailable {
		t.Fatalf("unconfigured = %v", err)
	}
	c, seen := providerClient(t, func(chatRequest) string {
		return `{"title":"restart caddy","language":"bash","body":"sudo systemctl restart caddy {{host}}","notes":"does it"}`
	})
	l := Local{Client: c}
	if _, err := l.Generate(ctx, "   ", ai.KindCommand, ""); err == nil || err.Error() != "prompt is required" {
		t.Fatalf("empty prompt = %v", err)
	}
	if _, err := l.Generate(ctx, strings.Repeat("x", 4001), ai.KindCommand, ""); err == nil ||
		err.Error() != "prompt is too long (max 4000 characters)" {
		t.Fatalf("long prompt = %v", err)
	}
	if _, err := l.Generate(ctx, "p", ai.Kind("weird"), ""); err == nil ||
		err.Error() != "kind must be command, script, or function" {
		t.Fatalf("bad kind = %v", err)
	}
	// Happy path: the language rides along, the template flag derives.
	got, err := l.Generate(ctx, "restart caddy", "", "bash")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if got.Title != "restart caddy" || got.Language != "bash" || !got.UsesVariables ||
		got.Body != "sudo systemctl restart caddy {{host}}" || got.Notes != "does it" {
		t.Fatalf("generation = %+v", got)
	}
	req := (*seen)[len(*seen)-1]
	// The language instruction rides the same user message as the
	// prompt (ai.Generate appends it), so match on the prefix.
	if !strings.HasPrefix(req.Messages[1].Content, "restart caddy") ||
		!strings.Contains(req.Messages[1].Content, "bash") {
		t.Fatalf("user message = %q", req.Messages[1].Content)
	}
	// ParseKind("") made this a command, so the system prompt is the
	// command rule.
	if !strings.Contains(req.Messages[0].Content, "ONE executable command") {
		t.Fatalf("system prompt = %q", req.Messages[0].Content)
	}
}

func TestLocalSuggestTagsPolicies(t *testing.T) {
	ctx := context.Background()
	if _, err := (Local{}).SuggestTags(ctx, TagInput{Body: "b"}); err != ErrUnavailable {
		t.Fatalf("unconfigured = %v", err)
	}
	c, seen := providerClient(t, func(chatRequest) string {
		return `["python", "PYTHON", "bad tag", "net", "extra1", "extra2"]`
	})
	l := Local{Client: c}
	if _, err := l.SuggestTags(ctx, TagInput{Body: "b", Sensitive: true}); err != ErrSensitive {
		t.Fatalf("sensitive = %v", err)
	}
	if _, err := l.SuggestTags(ctx, TagInput{}); err == nil || err.Error() != "body is required" {
		t.Fatalf("empty body = %v", err)
	}
	// Model output is filtered to valid names, de-duplicated. Note the
	// pipeline: ai.SuggestTags already lowercases/dedupes/caps at 3
	// (parseTagList) BEFORE the grammar filter, so a junk token can
	// leave fewer than 3 — the server handler behaves the same.
	got, err := l.SuggestTags(ctx, TagInput{Body: "import os", Title: "t", Language: "python"})
	if err != nil {
		t.Fatalf("suggest: %v", err)
	}
	if len(got) != 2 || got[0] != "python" || got[1] != "net" {
		t.Fatalf("tags = %+v", got)
	}
	// All-valid names cap at 3.
	c2, _ := providerClient(t, func(chatRequest) string {
		return `["a", "b", "c", "d"]`
	})
	capped, err := Local{Client: c2}.SuggestTags(ctx, TagInput{Body: "b"})
	if err != nil || len(capped) != 3 || capped[2] != "c" {
		t.Fatalf("capped = %+v, %v", capped, err)
	}
	// The vocabulary the provider sees comes from the store, like the
	// server's handler.
	st, err := store.Open(filepath.Join(t.TempDir(), "snp.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.CreateSnippet(store.SnippetInput{Title: "a", Body: "x", Tags: []string{"ops", "bash"}}); err != nil {
		t.Fatal(err)
	}
	if err := l2Suggest(st, c, ctx, t, seen); err != nil {
		t.Fatal(err)
	}
}

func l2Suggest(st *store.Store, c *ai.Client, ctx context.Context, t *testing.T, seen *[]chatRequest) error {
	l := Local{Client: c, Store: st}
	if _, err := l.SuggestTags(ctx, TagInput{Body: "y"}); err != nil {
		return err
	}
	req := (*seen)[len(*seen)-1]
	// ListTags is count-ordered, so assert both names, not the order.
	if !strings.Contains(req.Messages[1].Content, "Existing tags in the collection:") ||
		!strings.Contains(req.Messages[1].Content, "ops") ||
		!strings.Contains(req.Messages[1].Content, "bash") {
		t.Fatalf("vocabulary not sent: %q", req.Messages[1].Content)
	}
	return nil
}

func TestLocalExplainPolicies(t *testing.T) {
	ctx := context.Background()
	if _, err := (Local{}).Explain(ctx, "b", false); err != ErrUnavailable {
		t.Fatalf("unconfigured = %v", err)
	}
	c, _ := providerClient(t, func(chatRequest) string { return "  restarts the web server  " })
	l := Local{Client: c}
	if _, err := l.Explain(ctx, "b", true); err != ErrSensitive {
		t.Fatalf("sensitive = %v", err)
	}
	if _, err := l.Explain(ctx, "  ", false); err == nil || err.Error() != "body is required" {
		t.Fatalf("empty body = %v", err)
	}
	notes, err := l.Explain(ctx, "systemctl restart", false)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	if notes != "restarts the web server" {
		t.Fatalf("notes = %q", notes)
	}
}

func TestNewHTTPRejects(t *testing.T) {
	for _, raw := range []string{"", "ftp://example.com", "snp.tailnet.ts.net", "http://"} {
		if _, err := NewHTTP(raw); err == nil {
			t.Errorf("NewHTTP(%q) succeeded", raw)
		}
	}
}

// snpServer fakes the /api/ai/* endpoint shapes the real server serves
// (internal/server/handlers.go).
func snpServer(t *testing.T, check func(method, path string, body []byte)) (string, *statusOut) {
	t.Helper()
	status := &statusOut{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body []byte
		if r.Body != nil {
			body, _ = io.ReadAll(r.Body)
		}
		if r.ContentLength > 0 {
			if ct := r.Header.Get("Content-Type"); ct != "application/json" {
				t.Errorf("content type = %q", ct)
			}
		}
		switch r.URL.Path {
		case "/api/ai/status":
			json.NewEncoder(w).Encode(status)
		case "/api/ai/generate":
			check(r.Method, r.URL.Path, body)
			json.NewEncoder(w).Encode(generateOut{
				Title: "gen", Language: "go", Body: "echo {{h}}", Notes: "n", UsesVariables: true,
			})
		case "/api/ai/tags":
			check(r.Method, r.URL.Path, body)
			json.NewEncoder(w).Encode(tagsOut{Tags: []string{"ops", "bash"}})
		case "/api/ai/explain":
			check(r.Method, r.URL.Path, body)
			json.NewEncoder(w).Encode(explainOut{Notes: "explains"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL, status
}

func TestHTTPSurface(t *testing.T) {
	base, status := snpServer(t, func(method, path string, body []byte) {})
	status.Enabled = true
	status.Model = "m"
	h, err := NewHTTP(base)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	st, err := h.Status(ctx)
	if err != nil || !st.Enabled || st.Model != "m" {
		t.Fatalf("status = %+v, %v", st, err)
	}

	var lastGen generateReq
	base2, _ := snpServer(t, func(method, path string, body []byte) {
		if path == "/api/ai/generate" {
			json.Unmarshal(body, &lastGen)
		}
	})
	h2, _ := NewHTTP(base2)
	got, err := h2.Generate(ctx, "a prompt", ai.KindScript, "go")
	if err != nil || got.Title != "gen" || got.Language != "go" || !got.UsesVariables ||
		got.Body != "echo {{h}}" || got.Notes != "n" {
		t.Fatalf("generate = %+v, %v", got, err)
	}
	if lastGen.Prompt != "a prompt" || lastGen.Kind != "script" || lastGen.Language != "go" {
		t.Fatalf("request = %+v", lastGen)
	}

	var lastTags tagsReq
	base3, _ := snpServer(t, func(method, path string, body []byte) {
		if path == "/api/ai/tags" {
			json.Unmarshal(body, &lastTags)
		}
	})
	h3, _ := NewHTTP(base3)
	tags, err := h3.SuggestTags(ctx, TagInput{Body: "b", Title: "t", Sensitive: false})
	if err != nil || len(tags) != 2 || tags[0] != "ops" {
		t.Fatalf("tags = %+v, %v", tags, err)
	}
	if lastTags.Body != "b" || lastTags.IsSensitive {
		t.Fatalf("tags request = %+v", lastTags)
	}

	var lastExplain explainReq
	base4, _ := snpServer(t, func(method, path string, body []byte) {
		if path == "/api/ai/explain" {
			json.Unmarshal(body, &lastExplain)
		}
	})
	h4, _ := NewHTTP(base4)
	notes, err := h4.Explain(ctx, "body text", true)
	if err != nil || notes != "explains" {
		t.Fatalf("explain = %q, %v", notes, err)
	}
	if lastExplain.Body != "body text" || !lastExplain.IsSensitive {
		t.Fatalf("explain request = %+v", lastExplain)
	}
}

func TestHTTPErrorTexts(t *testing.T) {
	// The server's own texts surface verbatim, matching Local's
	// sentinels — the same failure reads the same both ways.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ai/generate":
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{"error": "AI generation is not configured (set ai_key / SNP_AI_KEY)"})
		case "/api/ai/tags":
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{"error": "AI actions are unavailable for sensitive snippets"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	h, err := NewHTTP(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, err = h.Generate(ctx, "p", ai.KindCommand, "")
	if err == nil || err.Error() != ErrUnavailable.Error() {
		t.Fatalf("503 text = %v", err)
	}
	_, err = h.SuggestTags(ctx, TagInput{Body: "b", Sensitive: true})
	if err == nil || err.Error() != ErrSensitive.Error() {
		t.Fatalf("403 text = %v", err)
	}
}
