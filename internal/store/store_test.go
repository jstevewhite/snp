package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (f fakeClock) Now() time.Time { return f.t }

var baseTime = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

func ts(t time.Time) string {
	return t.UTC().Truncate(time.Second).Format("2006-01-02T15:04:05Z")
}

func newTestStore(t *testing.T) (*Store, *fakeClock) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(p)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	fc := &fakeClock{t: baseTime}
	s.SetClock(fc)
	k, err := LoadOrCreateKey(filepath.Join(t.TempDir(), "key"))
	if err != nil {
		t.Fatalf("LoadOrCreateKey: %v", err)
	}
	s.SetKey(k)
	return s, fc
}

func mustCreate(t *testing.T, s *Store, in SnippetInput) SnippetOut {
	t.Helper()
	out, err := s.CreateSnippet(in)
	if err != nil {
		t.Fatalf("CreateSnippet(%q): %v", in.Title, err)
	}
	return out
}

func strPtr(s string) *string { return &s }

func TestSnippetCRUD(t *testing.T) {
	s, fc := newTestStore(t)
	in := SnippetInput{
		Title: "restart caddy", Body: "sudo systemctl restart caddy",
		Language: "bash", Notes: "after Caddyfile changes",
		Tags: []string{"ops", "Caddy"},
	}
	created := mustCreate(t, s, in)
	if created.ID == "" {
		t.Fatal("no id")
	}
	if created.CreatedAt != ts(fc.t) || created.UpdatedAt != ts(fc.t) {
		t.Errorf("timestamps: %q / %q", created.CreatedAt, created.UpdatedAt)
	}
	if created.Body == nil || *created.Body != in.Body {
		t.Errorf("body: %v", created.Body)
	}
	if len(created.Tags) != 2 || created.Tags[0] != "ops" || created.Tags[1] != "caddy" {
		t.Errorf("tags not normalized: %v", created.Tags)
	}

	fc.t = fc.t.Add(time.Minute)
	updated, err := s.ReplaceSnippet(created.ID, SnippetInput{
		Title: "restart caddy (v2)", Body: in.Body, Language: "bash",
		Notes: in.Notes, Tags: in.Tags,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.CreatedAt != created.CreatedAt {
		t.Errorf("created_at changed: %q -> %q", created.CreatedAt, updated.CreatedAt)
	}
	if updated.UpdatedAt == created.UpdatedAt {
		t.Error("updated_at did not advance")
	}

	got, err := s.GetSnippet(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "restart caddy (v2)" {
		t.Errorf("title: %q", got.Title)
	}

	raw, err := s.RawBody(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if raw != in.Body {
		t.Errorf("raw: %q", raw)
	}

	if err := s.SoftDeleteSnippet(created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSnippet(created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("get after delete: %v", err)
	}
	list, err := s.ListSnippets(ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Errorf("list after delete: %d", len(list))
	}
	if err := s.SoftDeleteSnippet(created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("double delete: %v", err)
	}
}

func TestFolderIDValidation(t *testing.T) {
	s, _ := newTestStore(t)
	_, err := s.CreateSnippet(SnippetInput{Title: "x", FolderID: strPtr("nope")})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestInvalidTag(t *testing.T) {
	s, _ := newTestStore(t)
	if _, err := s.CreateSnippet(SnippetInput{Title: "x", Tags: []string{"bad tag"}}); !errors.Is(err, ErrInvalidTag) {
		t.Errorf("expected ErrInvalidTag, got %v", err)
	}
	if _, err := s.CreateSnippet(SnippetInput{Title: "x", Tags: []string{"-leading"}}); !errors.Is(err, ErrInvalidTag) {
		t.Errorf("expected ErrInvalidTag, got %v", err)
	}
}

func TestEmptyTitle(t *testing.T) {
	s, _ := newTestStore(t)
	if _, err := s.CreateSnippet(SnippetInput{Title: "  "}); !errors.Is(err, ErrInvalid) {
		t.Errorf("expected ErrInvalid, got %v", err)
	}
}

func TestTagCounts(t *testing.T) {
	s, _ := newTestStore(t)
	mustCreate(t, s, SnippetInput{Title: "a", Body: "1", Tags: []string{"ops", "dev"}})
	mustCreate(t, s, SnippetInput{Title: "b", Body: "2", Tags: []string{"ops"}})
	mustCreate(t, s, SnippetInput{Title: "c", Body: "3"})

	counts, err := s.ListTags()
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != 2 {
		t.Fatalf("tags = %+v", counts)
	}
	if counts[0].Name != "ops" || counts[0].Count != 2 {
		t.Errorf("first = %+v", counts[0])
	}
	if counts[1].Name != "dev" || counts[1].Count != 1 {
		t.Errorf("second = %+v", counts[1])
	}
}
