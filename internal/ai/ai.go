// Package ai implements snp's one-shot snippet-generation feature
// (spec §13): a stateless call to an OpenAI-compatible chat
// completions endpoint that returns JSON describing a snippet ready for
// the frontend's Ask-AI control.
//
// Design constraints:
//   - One shot, no chat history: every request is a fresh system +
//     user message pair; nothing is stored server-side.
//   - The model is asked for a bare snippet (command, regex, fragment,
//     script, function) using snp's {{name}} / {{name|default}}
//     template syntax, in a JSON envelope, so the reply can be parsed
//     into the snippet form instead of prose. The caller picks the
//     shape via Kind, which selects the system prompt.
//   - The API key lives in config/env and is never exposed over the
//     snp API; request and response bodies are not logged.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jstevewhite/snp/internal/config"
)

// Kind selects the shape of the generated snippet (spec §13): a single
// command, a multi-line script, or a function definition. The zero
// value behaves as KindCommand.
type Kind string

const (
	KindCommand  Kind = "command"
	KindScript   Kind = "script"
	KindFunction Kind = "function"
)

// ParseKind normalizes a client-supplied kind. The empty string is the
// command default; an unrecognized token reports ok=false so the
// caller can reject it instead of silently generating a command.
func ParseKind(s string) (Kind, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", string(KindCommand):
		return KindCommand, true
	case string(KindScript):
		return KindScript, true
	case string(KindFunction):
		return KindFunction, true
	}
	return "", false
}

// promptIntro opens every generation prompt.
const promptIntro = `You generate snippets for "snp", a personal snippet manager.`

// commandRule is the body rule for KindCommand: exactly one line, one
// command (spec §13). A commented multi-command block is the failure
// mode this pins.
const commandRule = `The user asks for a command, a regex, a shell one-liner, a config
fragment, or similar. The "body" must be exactly ONE executable command
on a single line: no shell comments (#), no explanatory prose, no
second or alternative command, no markdown code fences, no surrounding
quotes. Pipes, redirections, and &&/|| joining inside that one command
are fine; blank lines and comment lines are not. If several commands
would help, put the single most useful one in "body" and describe the
alternatives, flags, gotchas, and caveats in "notes".`

// scriptRule is the body rule for KindScript: a complete runnable
// multi-line program, saved as one snippet.
const scriptRule = `The user asks for a script — a multi-line program saved as one snippet
and run as a file. The "body" holds the complete script: multiple lines
are expected. For a shell script, start with an appropriate shebang
(#!/usr/bin/env bash), and include the imports, argument handling, and
comments that make the script runnable and readable. Use plain comments
(#, //, and so on) inside the body where they help. Do not wrap the
body in markdown code fences and do not put prose outside the script.`

// functionRule is the body rule for KindFunction: one named function
// definition in the requested language.
const functionRule = `The user asks for a function — a single named function definition in
the requested language. The "body" holds the complete function:
signature, body, and any imports it needs (imports may precede it).
Multiple lines are expected. Follow the language's conventions —
naming, doc comment or docstring, type annotations where the language
uses them. Do not wrap the body in markdown code fences and do not
include a call-site example in the body.`

// promptShared carries the rules common to every kind: template
// placeholders, variable grammar, and the JSON envelope.
const promptShared = `
Use snp template placeholders in the body when the request is generic —
"a file", "<example>", "the path", "some name" — written as {{name}},
or {{name|default}} when one obvious default exists. Pick short,
lowercase, descriptive names.

Variable names must match [A-Za-z_][A-Za-z0-9_]* : start with a letter
or underscore, then only letters, digits, or underscores. NO hyphens,
spaces, dots, or other punctuation inside the braces — write
{{bundle_file}}, never {{bundle-file}}.

Respond with a single JSON object:
{"title": "short descriptive title, max 60 chars",
 "language": "lowercase language tag: bash, sh, zsh, python, regex,
              sql, go, js, ts, json, yaml, markdown, text, or another
              short token when obvious; empty string when unclear",
 "body": "<the snippet>",
 "notes": "<the explanation>"}
title and language must not contain newlines.`

// bodyRule returns the opening rule and the closing requirement for a
// kind; the closing line repeats what "body" must be, after the
// envelope, because models weight the last instruction heavily.
func bodyRule(kind Kind) (rule, tail string) {
	switch kind {
	case KindScript:
		return scriptRule, `body must be the complete runnable script, newlines included, with
no markdown code fences. notes must say what the script does and how
to run it (save as a file, chmod +x, arguments, and so on); short
Markdown lines are fine — notes are rendered as Markdown.`
	case KindFunction:
		return functionRule, `body must be the complete function definition, newlines included, with
no markdown code fences and no call-site example. notes must state the
language, the parameters, the return value, and a one-line example of
calling it; short Markdown lines are fine — notes are rendered as
Markdown.`
	default:
		return commandRule, `notes must stay a single plain-text line. body must be a single line
holding exactly one command — no comments, no additional commands —
valid to run as-is.`
	}
}

// systemPromptFor composes the system prompt for a kind. Unknown kinds
// fall back to the command prompt; the server rejects them earlier.
func systemPromptFor(kind Kind) string {
	rule, tail := bodyRule(kind)
	return promptIntro + "\n\n" + rule + "\n" + promptShared + "\n" + tail
}

const tagSuggestPrompt = `You suggest tags for a snippet in "snp", a personal
snippet manager. Given the snippet's body (and its optional title and
language) and the list of tags the user already has, return 2 or 3
short, relevant, lowercase tags. Reuse existing tags when they fit and
propose new ones freely (a tag that does not exist yet is fine, e.g.
"python" for a Python snippet).

Tag names must match [a-z0-9][a-z0-9-]{0,63}: lowercase letters, digits,
and hyphens only — no spaces, underscores, or other punctuation.

Respond with ONLY a JSON array of strings, e.g. ["python", "network"].`

const explainPrompt = `You explain a command or snippet for a personal snippet
manager. Reply in Markdown: one summary sentence, then a bulleted list of
gotchas and the most important information the user needs before running
it. No code fences. Keep the reply under 500 tokens (reasoning may use
more; only the final reply counts).`

// Result is the parsed generation: snippet fields for the editor.
type Result struct {
	Title    string
	Language string
	Body     string
	// Notes is a plain-text explanation of the snippet (spec §13); the
	// frontend puts it in the snippet's Notes field.
	Notes string
}

// Client talks to one OpenAI-compatible chat completions endpoint.
// Zero-value fields come from FromConfig; the fields are exported so
// tests can point the client at an httptest server.
type Client struct {
	Endpoint string
	Model    string
	APIKey   string
	HTTP     *http.Client
	Log      *slog.Logger
}

// FromConfig returns a client when the AI feature is configured (an
// API key is set), nil otherwise. Endpoint and model fall back to the
// config defaults (OpenAI-compatible base + model name).
func FromConfig(cfg config.Config, log *slog.Logger) *Client {
	if !cfg.AIEnabled() {
		return nil
	}
	return &Client{
		Endpoint: strings.TrimRight(cfg.AIEndpoint, "/"),
		Model:    cfg.AIModel,
		APIKey:   cfg.AIKey,
		HTTP:     &http.Client{Timeout: 60 * time.Second},
		Log:      log,
	}
}

// UpstreamError reports a non-2xx response from the AI endpoint.
type UpstreamError struct {
	Status int
	Detail string // short excerpt of the provider error body
}

func (e *UpstreamError) Error() string {
	return fmt.Sprintf("AI provider returned status %d", e.Status)
}

// OutputError reports a response that could not be parsed into a
// snippet (missing fields, invalid JSON, empty body).
type OutputError struct{ Reason string }

func (e *OutputError) Error() string { return "AI returned unparseable content" }

// ErrEmptyPrompt is returned for an empty prompt.
var ErrEmptyPrompt = fmt.Errorf("ai: empty prompt")

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

type snippetJSON struct {
	Title    string `json:"title"`
	Language string `json:"language"`
	Body     string `json:"body"`
	Notes    string `json:"notes"`
}

// TagSuggestParams is the context for TagSuggestions.
type TagSuggestParams struct {
	Body     string
	Title    string
	Language string
	// Existing is the user's current tag vocabulary (lowercase names),
	// so the model can reuse them or extend them.
	Existing []string
}

// GenerateParams is the input to Generate (spec §13).
type GenerateParams struct {
	Prompt string
	// Language, when non-empty, forces the language of the reply.
	Language string
	// Kind selects the snippet shape; the zero value is a single
	// command.
	Kind Kind
}

// Generate runs one stateless request for the given prompt. language,
// when non-empty, asks the model to tag the snippet with that language.
// Kind selects the system prompt (one command, a script, or a
// function).
func (c *Client) Generate(ctx context.Context, p GenerateParams) (Result, error) {
	if strings.TrimSpace(p.Prompt) == "" {
		return Result{}, ErrEmptyPrompt
	}
	user := p.Prompt
	if p.Language != "" {
		user += "\n\nPut the snippet in the language " + p.Language +
			" and set the JSON language field to exactly \"" + p.Language + "\"."
	}
	content, err := c.complete(ctx, systemPromptFor(p.Kind), user)
	if err != nil {
		return Result{}, err
	}
	return parseSnippet([]byte(content))
}

// SuggestTags returns 2-3 relevant tags for a snippet, reusing the
// user's existing tag vocabulary and extending it where the model sees
// fit (spec §13). The reply is normalized to lowercase, trimmed,
// de-duplicated, and capped at 3; the caller (server) re-validates
// each name against the tag grammar.
func (c *Client) SuggestTags(ctx context.Context, p TagSuggestParams) ([]string, error) {
	if strings.TrimSpace(p.Body) == "" {
		return nil, ErrEmptyPrompt
	}
	var b strings.Builder
	b.WriteString("Snippet body:\n" + p.Body)
	if p.Title != "" {
		b.WriteString("\nTitle: " + p.Title)
	}
	if p.Language != "" {
		b.WriteString("\nLanguage: " + p.Language)
	}
	if len(p.Existing) == 0 {
		b.WriteString("\nExisting tags in the collection: (none)")
	} else {
		b.WriteString("\nExisting tags in the collection: " + strings.Join(p.Existing, ", "))
	}
	content, err := c.complete(ctx, tagSuggestPrompt, b.String())
	if err != nil {
		return nil, err
	}
	return parseTagList(content), nil
}

// Explain returns a plain-text (Markdown) explanation of a
// command/snippet — what it does, gotchas, important info — for the
// snippet's Notes field (spec §13). No hard token cap: the prompt asks
// the model to keep the reply short, which leaves room for reasoning
// tokens instead of truncating them.
func (c *Client) Explain(ctx context.Context, body string) (string, error) {
	if strings.TrimSpace(body) == "" {
		return "", ErrEmptyPrompt
	}
	content, err := c.complete(ctx, explainPrompt, "Explain this command/snippet:\n"+body)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(content), nil
}

// complete performs one stateless chat-completions round trip and
// returns the assistant message content.
func (c *Client) complete(ctx context.Context, system, user string) (string, error) {
	payload, err := json.Marshal(chatRequest{
		Model: c.Model,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Temperature: 0,
	})
	if err != nil {
		return "", err
	}
	url := c.Endpoint
	if !strings.HasSuffix(url, "/chat/completions") {
		url += "/chat/completions"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	start := time.Now()
	res, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("ai: %w", err)
	}
	defer res.Body.Close()
	resBody, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("ai: read response: %w", err)
	}
	if c.Log != nil {
		c.Log.Info("ai request", "model", c.Model, "status", res.StatusCode,
			"duration_ms", time.Since(start).Milliseconds())
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", &UpstreamError{
			Status: res.StatusCode,
			Detail: truncate(string(resBody), 200),
		}
	}
	var chat chatResponse
	if err := json.Unmarshal(resBody, &chat); err != nil {
		return "", fmt.Errorf("ai: decode provider response: %w", err)
	}
	if len(chat.Choices) == 0 || chat.Choices[0].Message.Content == "" {
		return "", &OutputError{Reason: "provider returned no content"}
	}
	return chat.Choices[0].Message.Content, nil
}

// parseTagList extracts a JSON array of strings from a model reply,
// tolerating ``` fences and prose, then normalizes (lowercase/trim/
// dedupe) and caps at 3 tags.
func parseTagList(content string) []string {
	cand := strings.TrimSpace(content)
	if i := strings.IndexByte(cand, '['); i >= 0 {
		if j := strings.LastIndexByte(cand, ']'); j > i {
			cand = cand[i : j+1]
		}
	}
	var raw []string
	if err := json.Unmarshal([]byte(cand), &raw); err != nil {
		return nil
	}
	out := []string{}
	seen := map[string]bool{}
	for _, t := range raw {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || seen[t] {
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

// parseSnippet extracts the model's JSON object from the response.
// Real providers occasionally wrap the object in ```json fences or a
// sentence of prose, so parsing is tolerant: try the raw body, then a
// fenced block, then the outermost {...} span.
func parseSnippet(resBody []byte) (Result, error) {
	text := strings.TrimSpace(string(resBody))
	candidates := []string{text}
	if low := strings.ToLower(text); strings.Contains(low, "```") {
		for _, fenced := range extractFences(text) {
			candidates = append(candidates, fenced)
		}
	}
	var out snippetJSON
	var lastErr error
	for _, cand := range candidates {
		if i := strings.IndexByte(cand, '{'); i >= 0 {
			if j := strings.LastIndexByte(cand, '}'); j > i {
				cand = cand[i : j+1]
			}
		}
		if err := json.Unmarshal([]byte(cand), &out); err != nil {
			lastErr = err
			continue
		}
		out.Title = strings.TrimSpace(out.Title)
		out.Language = strings.TrimSpace(out.Language)
		if out.Body == "" && out.Title == "" {
			lastErr = &OutputError{Reason: "empty title and body"}
			continue
		}
		return Result{Title: out.Title, Language: out.Language, Body: out.Body, Notes: out.Notes}, nil
	}
	if lastErr == nil {
		lastErr = &OutputError{Reason: "no JSON object found"}
	}
	return Result{}, &OutputError{Reason: lastErr.Error()}
}

// extractFences returns the inside of every ```…``` code block.
func extractFences(text string) []string {
	var out []string
	for {
		open := strings.Index(text, "```")
		if open < 0 {
			return out
		}
		rest := text[open+3:]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			rest = rest[nl+1:]
		}
		close := strings.Index(rest, "```")
		if close < 0 {
			return append(out, rest)
		}
		out = append(out, rest[:close])
		text = rest[close+3:]
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
