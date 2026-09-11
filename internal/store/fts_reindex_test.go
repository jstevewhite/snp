package store

import "testing"

// TestFTSReindexOnReplace guards against the external-content FTS5 bug where
// rewriting a snippet's index row either left stale terms behind or corrupted
// the index ("database disk image is malformed"). After a replace, the old
// title/body terms must no longer be searchable and the new ones must be.
// It exercises consecutive replaces to confirm the index stays consistent.
func TestFTSReindexOnReplace(t *testing.T) {
	s, _ := newTestStore(t)
	created := mustCreate(t, s, SnippetInput{
		Title: "alpha gateway", Body: "zebra crossing", Language: "go",
	})

	// Baseline: freshly created content is searchable.
	if got, err := s.ListSnippets(ListFilter{Q: "zebra"}); err != nil || len(got) != 1 || got[0].ID != created.ID {
		t.Fatalf("before replace: want zebra hit on %s, got %+v (err %v)", created.ID, got, err)
	}

	// First replace: entirely different title and body.
	if _, err := s.ReplaceSnippet(created.ID, SnippetInput{
		Title: "beta router", Body: "quokka hop", Language: "bash",
	}); err != nil {
		t.Fatalf("replace #1: %v", err)
	}
	if got, err := s.ListSnippets(ListFilter{Q: "zebra"}); err != nil || len(got) != 0 {
		t.Errorf("old body term 'zebra' still searchable: %+v (err %v)", got, err)
	}
	if got, err := s.ListSnippets(ListFilter{Q: "alpha"}); err != nil || len(got) != 0 {
		t.Errorf("old title term 'alpha' still searchable: %+v (err %v)", got, err)
	}
	if got, err := s.ListSnippets(ListFilter{Q: "quokka"}); err != nil || len(got) != 1 || got[0].ID != created.ID {
		t.Errorf("new body term 'quokka' not searchable: %+v (err %v)", got, err)
	}
	if got, err := s.ListSnippets(ListFilter{Q: "beta"}); err != nil || len(got) != 1 || got[0].ID != created.ID {
		t.Errorf("new title term 'beta' not searchable: %+v (err %v)", got, err)
	}

	// Second replace: the index must stay consistent across consecutive updates.
	if _, err := s.ReplaceSnippet(created.ID, SnippetInput{
		Title: "gamma bridge", Body: "wombat dig", Language: "python",
	}); err != nil {
		t.Fatalf("replace #2: %v", err)
	}
	if got, err := s.ListSnippets(ListFilter{Q: "quokka"}); err != nil || len(got) != 0 {
		t.Errorf("stale body term 'quokka' still searchable after 2nd replace: %+v (err %v)", got, err)
	}
	if got, err := s.ListSnippets(ListFilter{Q: "beta"}); err != nil || len(got) != 0 {
		t.Errorf("stale title term 'beta' still searchable after 2nd replace: %+v (err %v)", got, err)
	}
	if got, err := s.ListSnippets(ListFilter{Q: "wombat"}); err != nil || len(got) != 1 || got[0].ID != created.ID {
		t.Errorf("new body term 'wombat' not searchable: %+v (err %v)", got, err)
	}
}
