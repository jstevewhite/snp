package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jstevewhite/snp/internal/config"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// fakeUpstream captures the provider request and serves a canned
// completion. The served content is the model's raw reply.
func fakeUpstream(t *testing.T, content string, status int) (*httptest.Server, func() map[string]any) {
	t.Helper()
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &captured)
		if status != http.StatusOK {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":{"message":"provider says no"}}`))
			return
		}
		payload := map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]any{"content": content}},
			},
		}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	t.Cleanup(srv.Close)
	return srv, func() map[string]any { return captured }
}

func testClient(t *testing.T, content string) (*Client, func() map[string]any) {
	t.Helper()
	srv, capture := fakeUpstream(t, content, http.StatusOK)
	c := FromConfig(config.Config{
		AIEndpoint: srv.URL,
		AIModel:    "test-model",
		AIKey:      "test-key",
	}, discard())
	c.HTTP = srv.Client()
	return c, capture
}

func TestFromConfigEnabledDisabled(t *testing.T) {
	if c := FromConfig(config.Config{}, discard()); c != nil {
		t.Error("no key -> client should be nil")
	}
	c := FromConfig(config.Config{AIEndpoint: "http://x", AIModel: "m", AIKey: "k"}, discard())
	if c == nil {
		t.Fatal("key set -> client expected")
	}
	if c.Endpoint != "http://x" || c.Model != "m" || c.APIKey != "k" {
		t.Errorf("client = %+v", c)
	}
}

func TestGenerateParsesSnippet(t *testing.T) {
	c, _ := testClient(t, `{"title":"Copy a file home","language":"bash","body":"cp -rf {{file}} ~/","notes":"Recursively copies the file to your home directory."}`)
	out, err := c.Generate(context.Background(), GenerateParams{Prompt: "give me a command to copy a file to my home dir"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Title != "Copy a file home" || out.Language != "bash" || out.Body != "cp -rf {{file}} ~/" {
		t.Errorf("result = %+v", out)
	}
	if out.Notes != "Recursively copies the file to your home directory." {
		t.Errorf("notes = %q", out.Notes)
	}
}

func TestGenerateNotesOptional(t *testing.T) {
	// Older/leaner models may omit the notes key; that must parse fine.
	c, _ := testClient(t, `{"title":"T","language":"","body":"echo hi"}`)
	out, err := c.Generate(context.Background(), GenerateParams{Prompt: "greet"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Notes != "" {
		t.Errorf("notes = %q, want empty", out.Notes)
	}
}

func TestGenerateRequestShape(t *testing.T) {
	c, capture := testClient(t, `{"title":"T","language":"","body":"echo hi"}`)
	if _, err := c.Generate(context.Background(), GenerateParams{Prompt: "a one-liner that greets", Language: "bash"}); err != nil {
		t.Fatal(err)
	}
	req := capture()
	if req["model"] != "test-model" {
		t.Errorf("model = %v", req["model"])
	}
	if temp, ok := req["temperature"].(float64); !ok || temp != 0 {
		t.Errorf("temperature = %v, want 0", req["temperature"])
	}
	msgs, ok := req["messages"].([]any)
	if !ok || len(msgs) != 2 {
		t.Fatalf("messages = %v, want exactly 2 (one shot, no history)", req["messages"])
	}
	sys := msgs[0].(map[string]any)
	user := msgs[1].(map[string]any)
	if sys["role"] != "system" || !strings.Contains(sys["content"].(string), "{{name|default}}") {
		t.Errorf("system message wrong: %v", sys["content"])
	}
	// The prompt must pin the variable-name grammar (letters/digits/
	// underscores only) so the model never emits e.g. {{bundle-file}}.
	if !strings.Contains(sys["content"].(string), "never {{bundle-file}}") {
		t.Errorf("system message does not forbid hyphenated variable names: %v", sys["content"])
	}
	// BODY must be exactly one single-line command (the model otherwise
	// returns a commented multi-command block).
	if !strings.Contains(sys["content"].(string), "exactly ONE executable command") ||
		!strings.Contains(sys["content"].(string), "no shell comments") {
		t.Errorf("system message does not pin the one-command body: %v", sys["content"])
	}
	if user["role"] != "user" {
		t.Errorf("user role = %v", user["role"])
	}
	u := user["content"].(string)
	if !strings.Contains(u, "a one-liner that greets") || !strings.Contains(u, `"bash"`) {
		t.Errorf("user content = %q", u)
	}
}

func TestParseKind(t *testing.T) {
	cases := map[string]Kind{
		"":         KindCommand,
		"command":  KindCommand,
		"COMMAND":  KindCommand,
		" script ": KindScript,
		"Script":   KindScript,
		"function": KindFunction,
	}
	for in, want := range cases {
		got, ok := ParseKind(in)
		if !ok || got != want {
			t.Errorf("ParseKind(%q) = %q, %v; want %q, true", in, got, ok, want)
		}
	}
	if _, ok := ParseKind("snippet"); ok {
		t.Error("unknown kind must be rejected")
	}
}

func TestGenerateKindPrompts(t *testing.T) {
	// Each kind picks its own body rule (spec §13); the shared half of
	// the prompt (template grammar, JSON envelope) is the same.
	cases := []struct {
		kind   Kind
		marker string
	}{
		{KindCommand, "exactly ONE executable command"},
		{KindScript, "a multi-line program saved as one snippet"},
		{KindFunction, "a single named function definition"},
	}
	for _, tc := range cases {
		c, capture := testClient(t, `{"title":"T","language":"bash","body":"x"}`)
		if _, err := c.Generate(context.Background(), GenerateParams{Prompt: "p", Kind: tc.kind}); err != nil {
			t.Fatalf("kind %q: %v", tc.kind, err)
		}
		msgs := capture()["messages"].([]any)
		sys := msgs[0].(map[string]any)["content"].(string)
		if !strings.Contains(sys, tc.marker) {
			t.Errorf("kind %q: prompt lacks %q:\n%s", tc.kind, tc.marker, sys)
		}
		if !strings.Contains(sys, "{{name|default}}") {
			t.Errorf("kind %q: prompt lost the shared template rules", tc.kind)
		}
	}
}

func TestGenerateScriptMultiLineBody(t *testing.T) {
	// The one-line rule is a command-mode prompt rule, not a parser
	// rule: a script reply keeps its newlines.
	script := "#!/usr/bin/env bash\nset -euo pipefail\necho hi"
	c, _ := testClient(t, `{"title":"Greet","language":"bash","body":"#!/usr/bin/env bash\nset -euo pipefail\necho hi"}`)
	out, err := c.Generate(context.Background(), GenerateParams{Prompt: "a greeting script", Kind: KindScript})
	if err != nil {
		t.Fatal(err)
	}
	if out.Body != script {
		t.Errorf("body = %q, want %q", out.Body, script)
	}
}

func TestGenerateTolerantParse(t *testing.T) {
	cases := []string{
		// Fenced JSON.
		"```json\n{\"title\":\"Fenced\",\"language\":\"regex\",\"body\":\"a{1,3}\"}\n```",
		// Prose before the object.
		"Here you go:\n{\"title\":\"Prose\",\"language\":\"\",\"body\":\"ls\"}",
	}
	for i, content := range cases {
		c, _ := testClient(t, content)
		out, err := c.Generate(context.Background(), GenerateParams{Prompt: "p"})
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		if out.Body == "" {
			t.Errorf("case %d: empty body", i)
		}
	}
}

func TestGenerateEmptyPrompt(t *testing.T) {
	c, _ := testClient(t, `{"title":"T","language":"","body":"x"}`)
	if _, err := c.Generate(context.Background(), GenerateParams{Prompt: "  "}); !errors.Is(err, ErrEmptyPrompt) {
		t.Errorf("err = %v, want ErrEmptyPrompt", err)
	}
}

func TestGenerateUpstreamErrors(t *testing.T) {
	for _, status := range []int{401, 429, 500} {
		srv, _ := fakeUpstream(t, "", status)
		c := FromConfig(config.Config{AIEndpoint: srv.URL, AIModel: "m", AIKey: "k"}, discard())
		c.HTTP = srv.Client()
		_, err := c.Generate(context.Background(), GenerateParams{Prompt: "p"})
		var ue *UpstreamError
		if !errors.As(err, &ue) {
			t.Fatalf("status %d: err = %v, want *UpstreamError", status, err)
		}
		if ue.Status != status {
			t.Errorf("status %d: ue.Status = %d", status, ue.Status)
		}
	}
}

func TestGenerateUnparseableOutput(t *testing.T) {
	for _, content := range []string{"just some words", "{}", ""} {
		c, _ := testClient(t, content)
		_, err := c.Generate(context.Background(), GenerateParams{Prompt: "p"})
		var oe *OutputError
		if !errors.As(err, &oe) {
			t.Errorf("content %q: err = %v, want *OutputError", content, err)
		}
	}
}

func TestEndpointJoining(t *testing.T) {
	// An endpoint that already ends in /chat/completions must not get a
	// second suffix appended.
	srv, _ := fakeUpstream(t, `{"title":"T","language":"","body":"x"}`, http.StatusOK)
	c := FromConfig(config.Config{AIEndpoint: srv.URL + "/chat/completions", AIModel: "m", AIKey: "k"}, discard())
	c.HTTP = srv.Client()
	if _, err := c.Generate(context.Background(), GenerateParams{Prompt: "p"}); err != nil {
		t.Errorf("already-suffixed endpoint failed: %v", err)
	}
}

func TestSuggestTags(t *testing.T) {
	c, capture := testClient(t, `["Python","network","python","deploy"]`)
	got, err := c.SuggestTags(context.Background(), TagSuggestParams{
		Body:     "python -m http.server",
		Title:    "Serve a dir",
		Language: "python",
		Existing: []string{"python", "script"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Lowercased, de-duplicated, capped at 3.
	if len(got) != 3 || got[0] != "python" || got[1] != "network" || got[2] != "deploy" {
		t.Errorf("tags = %v", got)
	}
	req := capture()
	msgs := req["messages"].([]any)
	sys := msgs[0].(map[string]any)["content"].(string)
	user := msgs[1].(map[string]any)["content"].(string)
	if !strings.Contains(sys, "a-z0-9") {
		t.Errorf("system prompt lacks tag grammar: %q", sys)
	}
	if !strings.Contains(user, "python -m http.server") ||
		!strings.Contains(user, "python, script") ||
		!strings.Contains(user, "Serve a dir") {
		t.Errorf("user message = %q", user)
	}
}

func TestSuggestTagsFenced(t *testing.T) {
	c, _ := testClient(t, "```json\n[\"a\", \"b\"]\n```")
	got, err := c.SuggestTags(context.Background(), TagSuggestParams{Body: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("tags = %v", got)
	}
}

func TestSuggestTagsEmptyBody(t *testing.T) {
	c, _ := testClient(t, `["a"]`)
	if _, err := c.SuggestTags(context.Background(), TagSuggestParams{Body: "  "}); !errors.Is(err, ErrEmptyPrompt) {
		t.Errorf("err = %v, want ErrEmptyPrompt", err)
	}
}

func TestExplain(t *testing.T) {
	c, capture := testClient(t, "It copies the file recursively; beware it overwrites.")
	got, err := c.Explain(context.Background(), "cp -rf {{file}} ~/")
	if err != nil {
		t.Fatal(err)
	}
	if got != "It copies the file recursively; beware it overwrites." {
		t.Errorf("explain = %q", got)
	}
	req := capture()
	if _, ok := req["max_tokens"]; ok {
		t.Errorf("max_tokens must not be set (prompt caps the reply, not the API)")
	}
	msgs := req["messages"].([]any)
	sys := msgs[0].(map[string]any)["content"].(string)
	user := msgs[1].(map[string]any)["content"].(string)
	if !strings.Contains(sys, "gotchas") || !strings.Contains(sys, "Markdown") {
		t.Errorf("system prompt = %q", sys)
	}
	if !strings.Contains(user, "cp -rf {{file}} ~/") {
		t.Errorf("user message = %q", user)
	}
}

func TestExplainEmptyBody(t *testing.T) {
	c, _ := testClient(t, "nope")
	if _, err := c.Explain(context.Background(), "  "); !errors.Is(err, ErrEmptyPrompt) {
		t.Errorf("err = %v, want ErrEmptyPrompt", err)
	}
}
