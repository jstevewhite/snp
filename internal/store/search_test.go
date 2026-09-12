package store

import (
	"fmt"
	"testing"
)

func TestParseQuery(t *testing.T) {
	terms, tags, langs := ParseQuery("tag:ops lang:bash restart the service")
	if len(terms) != 3 || terms[0] != "restart" || terms[2] != "service" {
		t.Errorf("terms = %v", terms)
	}
	if len(tags) != 1 || tags[0] != "ops" {
		t.Errorf("tags = %v", tags)
	}
	if len(langs) != 1 || langs[0] != "bash" {
		t.Errorf("langs = %v", langs)
	}
	terms, tags, langs = ParseQuery("tag:a tag:b lang:c")
	if len(terms) != 0 || len(tags) != 2 || len(langs) != 1 {
		t.Errorf("terms=%v tags=%v langs=%v", terms, tags, langs)
	}
}

func TestPrefixExpr(t *testing.T) {
	// Every term becomes a quoted prefix query; FTS5 ANDs them.
	if got := prefixExpr([]string{"zeb", "deploy"}); got != `"zeb"* "deploy"*` {
		t.Errorf("prefixExpr = %q", got)
	}
	// Embedded quotes are doubled, so the term stays one literal string.
	if got := prefixExpr([]string{`a"b`}); got != `"a""b"*` {
		t.Errorf("prefixExpr escaping = %q", got)
	}
	// A term with no letters or digits cannot prefix a token, so it is
	// dropped instead of emitted as an empty phrase (which FTS5 rejects).
	if got := prefixExpr([]string{"!!!", "ok"}); got != `"ok"*` {
		t.Errorf("prefixExpr drop = %q", got)
	}
	if got := prefixExpr(nil); got != "" {
		t.Errorf("prefixExpr(nil) = %q", got)
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

	// Every term is a prefix query, so a partial word finds its token —
	// in the body ("zeb" → "zebra crossing") or the title ("alp" →
	// "alpha one").
	got, err = s.ListSnippets(ListFilter{Q: "zeb"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "alpha one" {
		t.Errorf("prefix in body: got %+v", got)
	}
	got, err = s.ListSnippets(ListFilter{Q: "alp"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "alpha one" {
		t.Errorf("prefix in title: got %+v", got)
	}

	// A prefix only matches the start of a token, not the middle.
	got, err = s.ListSnippets(ListFilter{Q: "ebr"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("infix must not match: got %+v", got)
	}

	// Terms are ANDed: one term that matches nothing drops the row, even
	// though the other term does match it. (The offline engine must agree
	// — see web/src/lib/search.test.ts, same fixture.)
	got, err = s.ListSnippets(ListFilter{Q: "zebra nonexistentword"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("terms must AND: got %+v", got)
	}
}

func TestFTSMetacharactersAreLiteral(t *testing.T) {
	s, _ := newTestStore(t)
	mustCreate(t, s, SnippetInput{Title: "x y z", Body: "x y z"})
	// `x:y` would be a column filter if it reached FTS5 raw. Terms are
	// quoted, so it is literal text instead: the tokenizer reduces it to
	// `x` then `y`, which prefixes the title as a phrase. No syntax error,
	// and no driver down the fallback path.
	got, err := s.ListSnippets(ListFilter{Q: "x:y"})
	if err != nil {
		t.Fatalf("ListSnippets: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("got %d results, want 1", len(got))
	}
}

func TestFTSQuoteInTermIsLiteral(t *testing.T) {
	s, _ := newTestStore(t)
	mustCreate(t, s, SnippetInput{Title: "abc def", Body: "plain"})
	// An unterminated quote is just a character: the term is quoted and
	// escaped (`"abc"""*`), which the tokenizer reduces to the token `abc`
	// and which prefixes the title, without error.
	got, err := s.ListSnippets(ListFilter{Q: `abc"`})
	if err != nil {
		t.Fatalf("ListSnippets: %v", err)
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
