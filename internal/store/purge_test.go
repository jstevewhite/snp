package store

import (
	"errors"
	"testing"
	"time"
)

func TestPurge(t *testing.T) {
	s, fc := newTestStore(t)
	a := mustCreate(t, s, SnippetInput{Title: "a", Body: "1", Tags: []string{"doomed"}})

	fc.t = fc.t.Add(time.Hour)
	if err := s.SoftDeleteSnippet(a.ID); err != nil {
		t.Fatal(err)
	}

	// 10 days: not yet purgeable.
	fc.t = fc.t.Add(10 * 24 * time.Hour)
	c, err := s.Purge()
	if err != nil {
		t.Fatal(err)
	}
	if c.Snippets != 0 || c.Tags != 0 {
		t.Errorf("early purge = %+v", c)
	}
	// Still a tombstone.
	out, _ := s.SyncSince("")
	var tomb Tombstone
	for _, x := range out.Snippets {
		if tb, ok := x.(Tombstone); ok {
			tomb = tb
		}
	}
	if tomb.ID != a.ID {
		t.Errorf("tombstone missing: %+v", out.Snippets)
	}

	// Past 30 days: purged, tag pruned.
	fc.t = fc.t.Add(21 * 24 * time.Hour)
	c, err = s.Purge()
	if err != nil {
		t.Fatal(err)
	}
	if c.Snippets != 1 || c.Tags != 1 {
		t.Errorf("purge = %+v", c)
	}
	if _, err := s.GetSnippet(a.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("get after purge: %v", err)
	}
}

func TestPurgeKeepsReferencedTags(t *testing.T) {
	s, fc := newTestStore(t)
	a := mustCreate(t, s, SnippetInput{Title: "a", Body: "1", Tags: []string{"shared"}})
	b := mustCreate(t, s, SnippetInput{Title: "b", Body: "2", Tags: []string{"shared"}})

	fc.t = fc.t.Add(time.Hour)
	if err := s.SoftDeleteSnippet(a.ID); err != nil {
		t.Fatal(err)
	}
	fc.t = fc.t.Add(31 * 24 * time.Hour)
	c, err := s.Purge()
	if err != nil {
		t.Fatal(err)
	}
	if c.Snippets != 1 {
		t.Errorf("purge = %+v", c)
	}
	if c.Tags != 0 {
		t.Errorf("shared tag must survive: %+v", c)
	}
	counts, _ := s.ListTags()
	if len(counts) != 1 || counts[0].Name != "shared" || counts[0].Count != 1 {
		t.Errorf("tags = %+v", counts)
	}
	_ = b
}
