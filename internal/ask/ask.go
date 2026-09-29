// Package ask is the AI feature surface for snp's terminal clients
// (spec §13): status, generation, tag suggestions, and explanations,
// over two transports. Local runs the provider in-process through
// internal/ai and applies the same policy the server applies to
// /api/ai/* (internal/server/handlers.go), so the CLI is not a weaker
// twin; HTTP talks to a snp server's endpoints and the server enforces
// that policy itself. The error texts are the contract: both transports
// surface the server's own messages, and the tests pin them equal.
package ask

import (
	"context"
	"errors"
	"strings"

	"github.com/jstevewhite/snp/internal/ai"
	"github.com/jstevewhite/snp/internal/store"
)

// ErrUnavailable is the message the server sends (503) when the feature
// is not configured. The literal mirrors internal/server/handlers.go —
// change both together.
var ErrUnavailable = errors.New("AI generation is not configured (set ai_key / SNP_AI_KEY)")

// ErrSensitive is the refusal the server sends (403) for a body marked
// sensitive. Mirrors internal/server/handlers.go allowAIBody.
var ErrSensitive = errors.New("AI actions are unavailable for sensitive snippets")

// Status mirrors GET /api/ai/status: whether the feature is configured
// and which model would be used. The endpoint and key are never exposed.
type Status struct {
	Enabled bool
	Model   string
}

// Generation mirrors POST /api/ai/generate's response: snippet fields
// for the editor plus the derived template flag.
type Generation struct {
	Title         string
	Language      string
	Body          string
	Notes         string
	UsesVariables bool
}

// TagInput is the suggest-tags request context. Sensitive is enforced
// by Local and sent by HTTP (the server enforces it there).
type TagInput struct {
	Body      string
	Title     string
	Language  string
	Sensitive bool
}

// Service is the AI feature surface over both transports.
type Service interface {
	Status(ctx context.Context) (Status, error)
	Generate(ctx context.Context, prompt string, kind ai.Kind, language string) (Generation, error)
	SuggestTags(ctx context.Context, in TagInput) ([]string, error)
	Explain(ctx context.Context, body string, sensitive bool) (string, error)
}

var (
	_ Service = Local{}
	_ Service = HTTP{}
)

// Local runs the provider in-process: what the server's handlers do,
// minus the HTTP. A nil Client means the feature is unconfigured; a nil
// Store means no tag vocabulary to reuse.
type Local struct {
	Client *ai.Client
	Store  *store.Store
}

func (l Local) Status(ctx context.Context) (Status, error) {
	if l.Client == nil {
		return Status{Enabled: false}, nil
	}
	return Status{Enabled: true, Model: l.Client.Model}, nil
}

// Generate applies the server's validation (validateAIReq) before
// calling the provider: the prompt is required, at most 4000
// characters, and the kind must be command, script, or function.
func (l Local) Generate(ctx context.Context, prompt string, kind ai.Kind, language string) (Generation, error) {
	if l.Client == nil {
		return Generation{}, ErrUnavailable
	}
	if strings.TrimSpace(prompt) == "" {
		return Generation{}, errors.New("prompt is required")
	}
	if len(prompt) > 4000 {
		return Generation{}, errors.New("prompt is too long (max 4000 characters)")
	}
	k, ok := ai.ParseKind(string(kind))
	if !ok {
		return Generation{}, errors.New("kind must be command, script, or function")
	}
	res, err := l.Client.Generate(ctx, ai.GenerateParams{
		Prompt:   prompt,
		Language: language,
		Kind:     k,
	})
	if err != nil {
		return Generation{}, err
	}
	return Generation{
		Title:         res.Title,
		Language:      res.Language,
		Body:          res.Body,
		Notes:         res.Notes,
		UsesVariables: strings.Contains(res.Body, "{{"),
	}, nil
}

// SuggestTags mirrors the server's handler: refuse sensitive, require a
// body, reuse the store's tag vocabulary, and filter model output to
// names the store would accept.
func (l Local) SuggestTags(ctx context.Context, in TagInput) ([]string, error) {
	if l.Client == nil {
		return nil, ErrUnavailable
	}
	if in.Sensitive {
		return nil, ErrSensitive
	}
	if strings.TrimSpace(in.Body) == "" {
		return nil, errors.New("body is required")
	}
	var existing []string
	if l.Store != nil {
		counts, err := l.Store.ListTags()
		if err != nil {
			return nil, err
		}
		existing = make([]string, len(counts))
		for i, tc := range counts {
			existing[i] = tc.Name
		}
	}
	res, err := l.Client.SuggestTags(ctx, ai.TagSuggestParams{
		Body:     in.Body,
		Title:    in.Title,
		Language: in.Language,
		Existing: existing,
	})
	if err != nil {
		return nil, err
	}
	return filterTags(res), nil
}

// Explain mirrors the server's handler: refuse sensitive, require a
// body, one provider round trip.
func (l Local) Explain(ctx context.Context, body string, sensitive bool) (string, error) {
	if l.Client == nil {
		return "", ErrUnavailable
	}
	if sensitive {
		return "", ErrSensitive
	}
	if strings.TrimSpace(body) == "" {
		return "", errors.New("body is required")
	}
	return l.Client.Explain(ctx, body)
}

// filterTags normalizes model output to names the store would accept,
// de-duplicated, capped at 3 — the server handler's post-processing.
func filterTags(names []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, t := range names {
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
	return out
}
