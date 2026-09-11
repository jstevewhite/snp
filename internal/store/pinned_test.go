package store

import (
	"path/filepath"
	"testing"
)

// Spec §4: `pinned` is a plain, non-indexed, non-encrypted column, so it
// has to be threaded through every read path the app uses (get, list,
// sync, export) and both write paths (create, replace). A missed path
// shows up as a favorite that silently clears itself on the next sync.

func TestSnippetPinnedRoundTrip(t *testing.T) {
	s, _ := newTestStore(t)
	created := mustCreate(t, s, SnippetInput{
		Title: "deploy", Body: "kubectl apply -f k8s/", Language: "bash",
		Tags: []string{"ops"}, Pinned: true,
	})
	if !created.Pinned {
		t.Error("CreateSnippet: pinned not echoed back")
	}

	got, err := s.GetSnippet(created.ID)
	if err != nil {
		t.Fatalf("GetSnippet: %v", err)
	}
	if !got.Pinned {
		t.Error("GetSnippet: pinned lost")
	}

	listed, err := s.ListSnippets(ListFilter{Q: "deploy"})
	if err != nil {
		t.Fatalf("ListSnippets: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("ListSnippets: got %d rows, want 1", len(listed))
	}
	if !listed[0].Pinned {
		t.Error("ListSnippets: pinned lost")
	}

	synced, err := s.SyncSince("")
	if err != nil {
		t.Fatalf("SyncSince: %v", err)
	}
	var seen bool
	for _, item := range synced.Snippets {
		sn, ok := item.(SnippetOut)
		if !ok || sn.ID != created.ID {
			continue
		}
		seen = true
		if !sn.Pinned {
			t.Error("SyncSince: pinned lost")
		}
	}
	if !seen {
		t.Fatal("SyncSince: snippet missing from the response")
	}

	doc, err := s.Export()
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if len(doc.Snippets) != 1 {
		t.Fatalf("Export: got %d snippets, want 1", len(doc.Snippets))
	}
	if !doc.Snippets[0].Pinned {
		t.Error("Export: pinned lost")
	}
}

func TestReplaceSnippetPinned(t *testing.T) {
	s, _ := newTestStore(t)
	created := mustCreate(t, s, SnippetInput{
		Title: "deploy", Body: "kubectl apply -f k8s/", Pinned: true,
	})

	// Replace is a full write: un-pinning is expressed by sending false,
	// exactly as the app's pin toggle does.
	replaced := SnippetInput{
		Title: "deploy", Body: "kubectl apply -f k8s/", Pinned: false,
	}
	out, err := s.ReplaceSnippet(created.ID, replaced)
	if err != nil {
		t.Fatalf("ReplaceSnippet: %v", err)
	}
	if out.Pinned {
		t.Error("ReplaceSnippet: pinned not cleared")
	}
	if got, err := s.GetSnippet(created.ID); err != nil {
		t.Fatalf("GetSnippet: %v", err)
	} else if got.Pinned {
		t.Error("GetSnippet after replace: pinned still set")
	}

	// And back on again.
	replaced.Pinned = true
	if _, err := s.ReplaceSnippet(created.ID, replaced); err != nil {
		t.Fatalf("ReplaceSnippet: %v", err)
	}
	if got, err := s.GetSnippet(created.ID); err != nil {
		t.Fatalf("GetSnippet: %v", err)
	} else if !got.Pinned {
		t.Error("GetSnippet after re-pin: pinned lost")
	}
}

// A pin is not sensitive content, so it is stored plainly even for a
// sensitive snippet, whose body and defaults are encrypted.
func TestSensitiveSnippetPinnedStoredPlainly(t *testing.T) {
	s, _ := newTestStore(t)
	created := mustCreate(t, s, SnippetInput{
		Title: "token", Body: "hunter2", IsSensitive: true, Pinned: true,
	})
	if !created.Pinned {
		t.Error("CreateSnippet: pinned not echoed back for a sensitive snippet")
	}
	got, err := s.GetSnippet(created.ID)
	if err != nil {
		t.Fatalf("GetSnippet: %v", err)
	}
	if !got.Pinned {
		t.Error("GetSnippet: pinned lost for a sensitive snippet")
	}
	// The body stays hidden in list contexts; the pin still rides along.
	listed, err := s.ListSnippets(ListFilter{Q: "token"})
	if err != nil {
		t.Fatalf("ListSnippets: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("ListSnippets: got %d rows, want 1", len(listed))
	}
	if listed[0].Body != nil {
		t.Error("ListSnippets: sensitive body leaked")
	}
	if !listed[0].Pinned {
		t.Error("ListSnippets: pinned lost for a sensitive snippet")
	}
}

func TestExportImportPinned(t *testing.T) {
	s, _ := newTestStore(t)
	pinnedID := mustCreate(t, s, SnippetInput{
		Title: "deploy", Body: "kubectl apply -f k8s/", Pinned: true,
	}).ID
	plainID := mustCreate(t, s, SnippetInput{
		Title: "logs", Body: "journalctl -u snp -f", Pinned: false,
	}).ID

	doc, err := s.Export()
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	// Import into a fresh store (fresh key): the flags survive.
	p := filepath.Join(t.TempDir(), "fresh.db")
	fresh, err := Open(p)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { fresh.Close() })
	fresh.SetClock(&fakeClock{t: baseTime})
	fresh.SetKey(newKeyForTest(t))

	imp, err := jsonMarshalImportDoc(doc)
	if err != nil {
		t.Fatalf("jsonMarshalImportDoc: %v", err)
	}
	if _, err := fresh.Import(imp, "replace"); err != nil {
		t.Fatalf("Import: %v", err)
	}

	for _, want := range []struct {
		id     string
		pinned bool
	}{{pinnedID, true}, {plainID, false}} {
		got, err := fresh.GetSnippet(want.id)
		if err != nil {
			t.Fatalf("GetSnippet(%s): %v", want.id, err)
		}
		if got.Pinned != want.pinned {
			t.Errorf("imported %s: pinned = %v, want %v", want.id, got.Pinned, want.pinned)
		}
	}
}

// A document written before `pinned` existed carries no such field. It has
// to decode as false rather than fail — that is what happens when an older
// export is imported over a pinned row.
func TestImportOldDocWithoutPinned(t *testing.T) {
	s, _ := newTestStore(t)
	created := mustCreate(t, s, SnippetInput{
		Title: "deploy", Body: "kubectl apply -f k8s/", Pinned: true,
	})

	// The same row as an older export would carry it: the field is simply
	// absent, so decoding leaves it at its zero value.
	doc := ImportDoc{
		Version: 1,
		Snippets: []ImportSnippet{{
			ID: created.ID, Title: created.Title, Body: "kubectl apply -f k8s/",
		}},
	}
	if _, err := s.Import(doc, "replace"); err != nil {
		t.Fatalf("Import: %v", err)
	}
	got, err := s.GetSnippet(created.ID)
	if err != nil {
		t.Fatalf("GetSnippet: %v", err)
	}
	if got.Pinned {
		t.Error("a doc without the field must import as not pinned")
	}
}
