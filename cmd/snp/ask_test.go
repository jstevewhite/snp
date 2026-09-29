package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jstevewhite/snp/internal/ask"
)

// aiProvider fakes the OpenAI-compatible chat completions endpoint
// (internal/ai's wire format) and records the request.
func aiProvider(t *testing.T, reply string) (string, *chatReq) {
	t.Helper()
	seen := &chatReq{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(seen); err != nil {
			t.Errorf("decode provider request: %v", err)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]string{"content": reply},
			}},
		})
	}))
	t.Cleanup(srv.Close)
	return srv.URL, seen
}

type chatReq struct {
	Model    string           `json:"model"`
	Messages []map[string]any `json:"messages"`
}

func TestAskGenerateLocal(t *testing.T) {
	base, seen := aiProvider(t,
		`{"title":"restart caddy","language":"bash","body":"sudo systemctl restart caddy","notes":"does it"}`)
	cfg := testConfig(t)
	cfg.AIEndpoint = base
	cfg.AIModel = "test-model"
	cfg.AIKey = "k"

	gen, err := askGenerate(context.Background(), cfg, false, "restart caddy", "command", "")
	if err != nil {
		t.Fatalf("askGenerate: %v", err)
	}
	if gen.Title != "restart caddy" || gen.Language != "bash" ||
		gen.Body != "sudo systemctl restart caddy" || gen.Notes != "does it" {
		t.Fatalf("gen = %+v", gen)
	}
	if len(seen.Messages) != 2 || seen.Messages[1]["content"] != "restart caddy" {
		t.Fatalf("provider saw %+v", seen.Messages)
	}
	// The prompt is the last user message; the system prompt picks the
	// kind's rule.
	if sys, _ := seen.Messages[0]["content"].(string); len(sys) == 0 {
		t.Fatal("no system prompt")
	}
}

func TestAskGeneratePolicies(t *testing.T) {
	base, _ := aiProvider(t, `{"title":"t","language":"","body":"","notes":""}`)
	cfg := testConfig(t)
	cfg.AIEndpoint = base
	cfg.AIKey = "k"
	ctx := context.Background()

	// An unknown kind is rejected with the service's message.
	if _, err := askGenerate(ctx, cfg, false, "p", "weird", ""); err == nil ||
		err.Error() != "kind must be command, script, or function" {
		t.Fatalf("bad kind = %v", err)
	}
	// A generation with no body is the web form's empty-snippet error.
	if _, err := askGenerate(ctx, cfg, false, "p", "command", ""); err == nil ||
		err.Error() != "AI returned an empty snippet" {
		t.Fatalf("empty check = %v", err)
	}
}

func TestAskGenerateDisabledAndForceLocal(t *testing.T) {
	cfg := testConfig(t)
	// No ai_key: the feature errors with the configured message, the
	// same text the server sends.
	if _, err := askGenerate(context.Background(), cfg, false, "p", "command", ""); err == nil ||
		err.Error() != "AI generation is not configured (set ai_key / SNP_AI_KEY)" {
		t.Fatalf("disabled = %v", err)
	}
	// --local ignores url: a dead URL with a working local provider
	// still generates.
	base, _ := aiProvider(t, `{"title":"t","language":"","body":"b","notes":""}`)
	cfg.AIEndpoint = base
	cfg.AIKey = "k"
	cfg.URL = "http://127.0.0.1:1"
	gen, err := askGenerate(context.Background(), cfg, true, "p", "command", "")
	if err != nil || gen.Body != "b" {
		t.Fatalf("forced local = %+v, %v", gen, err)
	}
}

func TestAskGenerateHTTPTransport(t *testing.T) {
	// With a url and no --local, the server's /api/ai/generate serves
	// the generation — the provider is never contacted.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ai/generate" {
			http.NotFound(w, r)
			return
		}
		var req map[string]string
		json.NewDecoder(r.Body).Decode(&req)
		if req["prompt"] != "a prompt" || req["kind"] != "script" {
			t.Errorf("request = %v", req)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"title": "gen", "language": "go", "body": "package main", "notes": "n", "uses_variables": true,
		})
	}))
	defer srv.Close()
	cfg := testConfig(t)
	cfg.URL = srv.URL

	gen, err := askGenerate(context.Background(), cfg, false, "a prompt", "script", "")
	if err != nil {
		t.Fatalf("askGenerate: %v", err)
	}
	if gen.Title != "gen" || gen.Body != "package main" || !gen.UsesVariables {
		t.Fatalf("gen = %+v", gen)
	}
}

func TestPrefillFromGeneration(t *testing.T) {
	// The web fill rules: body always; the rest only when the model
	// produced them (an empty string prefills nothing downstream).
	p := prefillFromGeneration(ask.Generation{Title: "t", Body: "b", Notes: "n"})
	if p.Title != "t" || p.Language != "" || p.Body != "b" || p.Notes != "n" {
		t.Fatalf("prefill = %+v", p)
	}
}
