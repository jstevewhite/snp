package store

import (
	"testing"
	"time"
)

func TestSyncFull(t *testing.T) {
	s, fc := newTestStore(t)
	if _, err := s.CreateFolder("ops", nil); err != nil {
		t.Fatal(err)
	}
	a := mustCreate(t, s, SnippetInput{Title: "a", Body: "1"})
	b := mustCreate(t, s, SnippetInput{Title: "b", Body: "2", IsSensitive: true})

	out, err := s.SyncSince("")
	if err != nil {
		t.Fatal(err)
	}
	if out.ServerTime != ts(fc.t) {
		t.Errorf("server_time = %q, want %q", out.ServerTime, ts(fc.t))
	}
	if len(out.Folders) != 1 {
		t.Errorf("folders = %d", len(out.Folders))
	}
	if len(out.Snippets) != 2 {
		t.Errorf("snippets = %d", len(out.Snippets))
	}
	var foundA, foundB bool
	for _, x := range out.Snippets {
		so, ok := x.(SnippetOut)
		if !ok {
			continue
		}
		if so.ID == a.ID {
			foundA = true
			if so.Body == nil || *so.Body != "1" {
				t.Errorf("a body = %v", so.Body)
			}
		}
		if so.ID == b.ID {
			foundB = true
			if so.Body != nil {
				t.Error("sensitive body present in sync")
			}
			if so.Tags == nil {
				t.Error("tags missing in sync")
			}
		}
	}
	if !foundA || !foundB {
		t.Error("snippets missing from full sync")
	}
}

func TestSyncSince(t *testing.T) {
	s, fc := newTestStore(t)
	a := mustCreate(t, s, SnippetInput{Title: "a", Body: "1"})
	out0, err := s.SyncSince("")
	if err != nil {
		t.Fatal(err)
	}

	fc.t = fc.t.Add(time.Hour)
	if _, err := s.ReplaceSnippet(a.ID, SnippetInput{Title: "a2", Body: "1"}); err != nil {
		t.Fatal(err)
	}

	out1, err := s.SyncSince(out0.ServerTime)
	if err != nil {
		t.Fatal(err)
	}
	if out1.ServerTime != ts(fc.t) {
		t.Errorf("server_time = %q", out1.ServerTime)
	}
	var found bool
	for _, x := range out1.Snippets {
		if so, ok := x.(SnippetOut); ok && so.ID == a.ID {
			found = true
			if so.Title != "a2" {
				t.Errorf("title = %q", so.Title)
			}
		}
	}
	if !found {
		t.Error("updated snippet missing from sync")
	}
	if len(out1.Snippets) != 1 {
		t.Errorf("snippets = %d, want 1", len(out1.Snippets))
	}
	if len(out1.Folders) != 0 {
		t.Errorf("folders = %d, want 0", len(out1.Folders))
	}
}

func TestSyncTombstones(t *testing.T) {
	s, fc := newTestStore(t)
	a := mustCreate(t, s, SnippetInput{Title: "a", Body: "1"})
	f, _ := s.CreateFolder("f", nil)
	out0, err := s.SyncSince("")
	if err != nil {
		t.Fatal(err)
	}

	fc.t = fc.t.Add(time.Hour)
	if err := s.SoftDeleteSnippet(a.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteFolder(f.ID); err != nil {
		t.Fatal(err)
	}

	out, err := s.SyncSince(out0.ServerTime)
	if err != nil {
		t.Fatal(err)
	}
	var snTomb, fTomb Tombstone
	for _, x := range out.Snippets {
		if tb, ok := x.(Tombstone); ok {
			snTomb = tb
		}
	}
	for _, x := range out.Folders {
		if tb, ok := x.(Tombstone); ok {
			fTomb = tb
		}
	}
	if snTomb.ID != a.ID || snTomb.DeletedAt == "" {
		t.Errorf("snippet tombstone = %+v", snTomb)
	}
	if fTomb.ID != f.ID || fTomb.DeletedAt == "" {
		t.Errorf("folder tombstone = %+v", fTomb)
	}

	list, _ := s.ListSnippets(ListFilter{Limit: 200})
	if len(list) != 0 {
		t.Errorf("list = %d", len(list))
	}
	folders, _ := s.ListFolders()
	if len(folders) != 0 {
		t.Errorf("folders = %d", len(folders))
	}
}

// TestSyncSameSecondBoundary pins the >= boundary rule: timestamps are
// whole seconds (spec §4), so a row written in the same second as the
// previous sync snapshot has updated_at == that snapshot's server_time
// and must still be returned by the next incremental sync. A strict >
// would skip it forever (the row's timestamp never changes again).
func TestSyncSameSecondBoundary(t *testing.T) {
	s, fc := newTestStore(t)

	// A full sync at time T captures server_time = T.
	full, err := s.SyncSince("")
	if err != nil {
		t.Fatal(err)
	}
	// The clock does not move: the writes below land in the same second T.
	a := mustCreate(t, s, SnippetInput{Title: "a", Body: "1"})
	b := mustCreate(t, s, SnippetInput{Title: "b", Body: "2"})
	if err := s.SoftDeleteSnippet(b.ID); err != nil {
		t.Fatal(err)
	}
	if full.ServerTime != ts(fc.t) || a.UpdatedAt != full.ServerTime {
		t.Fatalf("fixture times off: server_time=%q a.updated_at=%q", full.ServerTime, a.UpdatedAt)
	}

	// The next incremental sync (since = T) must deliver the live row AND
	// the tombstone created in the same second as the snapshot.
	out, err := s.SyncSince(full.ServerTime)
	if err != nil {
		t.Fatal(err)
	}
	var live, tomb bool
	for _, x := range out.Snippets {
		switch v := x.(type) {
		case SnippetOut:
			if v.ID == a.ID {
				live = true
			}
		case Tombstone:
			if v.ID == b.ID {
				tomb = true
			}
		}
	}
	if !live {
		t.Error("same-second create missing from the next incremental sync")
	}
	if !tomb {
		t.Error("same-second delete missing from the next incremental sync")
	}

	// Duplicates are bounded: once the clock advances past T, the next
	// sync's server_time is > T and the row is not re-sent from then on.
	fc.t = fc.t.Add(time.Minute)
	out2, err := s.SyncSince(out.ServerTime)
	if err != nil {
		t.Fatal(err)
	}
	late, err := s.SyncSince(out2.ServerTime)
	if err != nil {
		t.Fatal(err)
	}
	if len(late.Snippets) != 0 {
		t.Errorf("rows re-sent after the boundary passed: %d", len(late.Snippets))
	}
}
