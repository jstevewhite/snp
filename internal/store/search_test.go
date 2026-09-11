package store

import (
	"fmt"
	"testing"
)

func TestParseQuery(t *testing.T) {
	fts, tags, langs := ParseQuery("tag:ops lang:bash restart the service")
	if fts != "restart the service" {
		t.Errorf("fts = %q", fts)
	}
	if len(tags) != 1 || tags[0] != "ops" {
		t.Errorf("tags = %v", tags)
	}
	if len(langs) != 1 || langs[0] != "bash" {
		t.Errorf("langs = %v", langs)
	}
	fts, tags, langs = ParseQuery("tag:a tag:b lang:c")
	if fts != "" || len(tags) != 2 || len(langs) != 1 {
		t.Errorf("fts=%q tags=%v langs=%v", fts, tags, langs)
	}
}

func TestFTSMatch(t *testing.T) {
	s, _ := newTestStore(t)
	mustCreate(t, s, SnippetInput{Title: "alpha one", Body: "zebra crossing", Language: "go"})
	mustCreate(t, s, SnippetInput{Title: "beta two", Body: "quokka hop", Language: "bash"})
	mustCreate(t, s, SnippetInput{Title: "gamma three", Body: "wombat dig", Language: "python"})

	got, err := s.ListSnippets(ListFilter{Q: "zebra"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "alpha one" {
		t.Errorf("got %+v", got)
	}

	// Phrase search.
	got, err = s.ListSnippets(ListFilter{Q: "wombat dig"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "gamma three" {
		t.Errorf("got %+v", got)
	}

	// No match.
	got, err = s.ListSnippets(ListFilter{Q: "nonexistentword"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("got %+v", got)
	}
}

func TestFTSFallbackQuotedPhrase(t *testing.T) {
	s, _ := newTestStore(t)
	mustCreate(t, s, SnippetInput{Title: "x y z", Body: "x y z"})
	// "x:y" is a column filter on a non-column: FTS5 rejects it raw,
	// so the quoted-phrase retry must find the literal text.
	got, err := s.ListSnippets(ListFilter{Q: "x:y"})
	if err != nil {
		t.Fatalf("fallback failed: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("got %d results, want 1", len(got))
	}
}

func TestFTSUnbalancedQuote(t *testing.T) {
	s, _ := newTestStore(t)
	mustCreate(t, s, SnippetInput{Title: "abc def", Body: "plain"})
	// Unterminated quote: raw MATCH fails; the quoted-phrase retry is the
	// phrase 'abc"', which the unicode61 tokenizer reduces to the token
	// 'abc' (the quote is a separator) and which matches the title,
	// without error.
	got, err := s.ListSnippets(ListFilter{Q: `abc"`})
	if err != nil {
		t.Fatalf("expected fallback, got %v", err)
	}
	if len(got) != 1 {
		t.Errorf("got %d results, want 1", len(got))
	}
}

func TestFilters(t *testing.T) {
	s, _ := newTestStore(t)
	f, _ := s.CreateFolder("ops", nil)
	mustCreate(t, s, SnippetInput{Title: "a", Body: "restart service", Language: "bash", Tags: []string{"ops"}, FolderID: &f.ID})
	mustCreate(t, s, SnippetInput{Title: "b", Body: "restart service", Language: "go", Tags: []string{"dev"}})

	got, err := s.ListSnippets(ListFilter{Tags: []string{"ops"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "a" {
		t.Errorf("tag filter: %+v", got)
	}

	got, _ = s.ListSnippets(ListFilter{Langs: []string{"go"}})
	if len(got) != 1 || got[0].Title != "b" {
		t.Errorf("lang filter: %+v", got)
	}

	got, _ = s.ListSnippets(ListFilter{Folder: &f.ID})
	if len(got) != 1 || got[0].Title != "a" {
		t.Errorf("folder filter: %+v", got)
	}

	got, _ = s.ListSnippets(ListFilter{Q: "tag:ops lang:bash restart"})
	if len(got) != 1 || got[0].Title != "a" {
		t.Errorf("q token filters: %+v", got)
	}

	// Incompatible filters AND together.
	got, _ = s.ListSnippets(ListFilter{Q: "tag:ops", Tags: []string{"dev"}})
	if len(got) != 0 {
		t.Errorf("ANDed filters: %+v", got)
	}

	// Only filter tokens: behaves as a plain filtered list.
	got, _ = s.ListSnippets(ListFilter{Q: "tag:ops lang:bash"})
	if len(got) != 1 || got[0].Title != "a" {
		t.Errorf("filter-only q: %+v", got)
	}
}

func TestSensitiveExcludedFromFTS(t *testing.T) {
	s, _ := newTestStore(t)
	created := mustCreate(t, s, SnippetInput{
		Title: "db password", Body: "hunter2secretbody", IsSensitive: true,
	})

	got, err := s.ListSnippets(ListFilter{Q: "hunter2secretbody"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Error("sensitive body is searchable")
	}

	got, err = s.ListSnippets(ListFilter{Q: "password"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("title should be searchable: %+v", got)
	}
	if got[0].Body != nil {
		t.Error("body must be null in list context")
	}

	full, err := s.GetSnippet(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if full.Body == nil || *full.Body != "hunter2secretbody" {
		t.Errorf("decrypted body = %v", full.Body)
	}
}

func TestPagination(t *testing.T) {
	s, _ := newTestStore(t)
	for i := 0; i < 60; i++ {
		mustCreate(t, s, SnippetInput{Title: fmt.Sprintf("snippet %02d", i), Body: "body"})
	}
	got, err := s.ListSnippets(ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 50 {
		t.Errorf("default limit: got %d, want 50", len(got))
	}
	got, _ = s.ListSnippets(ListFilter{Limit: 200})
	if len(got) != 60 {
		t.Errorf("limit 200: got %d, want 60", len(got))
	}
	got, _ = s.ListSnippets(ListFilter{Limit: 10, Offset: 10})
	if len(got) != 10 {
		t.Errorf("limit 10 offset 10: got %d", len(got))
	}
}
