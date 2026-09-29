package ask

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jstevewhite/snp/internal/ai"
)

// HTTP talks to a snp server's /api/ai/* endpoints. Identity comes from
// the tailnet source address, like every other snp client.
type HTTP struct {
	Base   *url.URL
	Client *http.Client
}

// NewHTTP builds a client for a snp base URL.
func NewHTTP(raw string) (HTTP, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return HTTP{}, fmt.Errorf("url must be an http or https address")
	}
	// Generation can take a while; the server-side provider timeout
	// is 60s, so the client must not give up first.
	return HTTP{Base: u, Client: &http.Client{Timeout: 75 * time.Second}}, nil
}

// The wire types mirror the server's request and response bodies
// (internal/server/handlers.go) — change both together.

type statusOut struct {
	Enabled bool   `json:"enabled"`
	Model   string `json:"model"`
}

type generateReq struct {
	Prompt   string `json:"prompt"`
	Language string `json:"language"`
	Kind     string `json:"kind"`
}

type generateOut struct {
	Title         string `json:"title"`
	Language      string `json:"language"`
	Body          string `json:"body"`
	Notes         string `json:"notes"`
	UsesVariables bool   `json:"uses_variables"`
}

type tagsReq struct {
	Body        string `json:"body"`
	Title       string `json:"title"`
	Language    string `json:"language"`
	IsSensitive bool   `json:"is_sensitive"`
}

type tagsOut struct {
	Tags []string `json:"tags"`
}

type explainReq struct {
	Body        string `json:"body"`
	IsSensitive bool   `json:"is_sensitive"`
}

type explainOut struct {
	Notes string `json:"notes"`
}

func (h HTTP) Status(ctx context.Context) (Status, error) {
	var out statusOut
	if err := h.call(ctx, http.MethodGet, "/api/ai/status", nil, &out); err != nil {
		return Status{}, err
	}
	return Status{Enabled: out.Enabled, Model: out.Model}, nil
}

func (h HTTP) Generate(ctx context.Context, prompt string, kind ai.Kind, language string) (Generation, error) {
	var out generateOut
	err := h.call(ctx, http.MethodPost, "/api/ai/generate", generateReq{
		Prompt:   prompt,
		Language: language,
		Kind:     string(kind),
	}, &out)
	if err != nil {
		return Generation{}, err
	}
	return Generation{
		Title:         out.Title,
		Language:      out.Language,
		Body:          out.Body,
		Notes:         out.Notes,
		UsesVariables: out.UsesVariables,
	}, nil
}

func (h HTTP) SuggestTags(ctx context.Context, in TagInput) ([]string, error) {
	var out tagsOut
	err := h.call(ctx, http.MethodPost, "/api/ai/tags", tagsReq{
		Body:        in.Body,
		Title:       in.Title,
		Language:    in.Language,
		IsSensitive: in.Sensitive,
	}, &out)
	if err != nil {
		return nil, err
	}
	return out.Tags, nil
}

func (h HTTP) Explain(ctx context.Context, body string, sensitive bool) (string, error) {
	var out explainOut
	err := h.call(ctx, http.MethodPost, "/api/ai/explain", explainReq{
		Body:        body,
		IsSensitive: sensitive,
	}, &out)
	if err != nil {
		return "", err
	}
	return out.Notes, nil
}

// call performs one JSON request and decodes the response into out.
// Errors carry the server's own message verbatim, unprefixed: the texts
// are the contract, and Local returns the same strings as sentinels —
// a prefix here would break that parity.
func (h HTTP) call(ctx context.Context, method, path string, payload any, out any) error {
	var body io.Reader
	if payload != nil {
		buf, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(buf)
	}
	u := h.Base.JoinPath(path)
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := h.Client.Do(req)
	if err != nil {
		return fmt.Errorf("ask: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("ask: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return bodyError(resp.StatusCode, data)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("ask: %w", err)
	}
	return nil
}

// bodyError surfaces the server's error message when it sent one; the
// texts are the same ones Local returns as sentinels, so a 503 reads
// identically in both transports.
func bodyError(status int, data []byte) error {
	var api struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &api) == nil && api.Error != "" {
		return errors.New(api.Error)
	}
	return fmt.Errorf("ask: HTTP %d", status)
}
