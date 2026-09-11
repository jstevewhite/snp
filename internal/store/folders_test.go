package store

import (
	"errors"
	"testing"
)

func TestFolderCreateListCollision(t *testing.T) {
	s, _ := newTestStore(t)
	f, err := s.CreateFolder("shell", nil)
	if err != nil {
		t.Fatal(err)
	}
	sub, err := s.CreateFolder("deploy", &f.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sub.ParentID == nil || *sub.ParentID != f.ID {
		t.Errorf("parent = %v", sub.ParentID)
	}
	if _, err := s.CreateFolder("Shell", nil); !errors.Is(err, ErrNameTaken) {
		t.Errorf("case-insensitive collision: %v", err)
	}
	if _, err := s.CreateFolder("deploy", &f.ID); !errors.Is(err, ErrNameTaken) {
		t.Errorf("sibling collision: %v", err)
	}
	// The same name under a different parent is fine.
	if _, err := s.CreateFolder("deploy", nil); err != nil {
		t.Errorf("different parent should be ok: %v", err)
	}
	list, err := s.ListFolders()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Errorf("folders = %d, want 3", len(list))
	}
}

func TestFolderMoveCycle(t *testing.T) {
	s, _ := newTestStore(t)
	a, _ := s.CreateFolder("a", nil)
	b, _ := s.CreateFolder("b", &a.ID)
	c, _ := s.CreateFolder("c", &b.ID)

	// Move a under c: cycle.
	if _, err := s.UpdateFolder(a.ID, nil, &c.ID); !errors.Is(err, ErrFolderCycle) {
		t.Errorf("expected cycle, got %v", err)
	}
	// Move c under itself: cycle.
	if _, err := s.UpdateFolder(c.ID, nil, &c.ID); !errors.Is(err, ErrFolderCycle) {
		t.Errorf("expected self-cycle, got %v", err)
	}
	// Move b to its current parent: legal no-op.
	if _, err := s.UpdateFolder(b.ID, nil, &a.ID); err != nil {
		t.Errorf("legal move: %v", err)
	}
	// Move c under a: legal.
	if _, err := s.UpdateFolder(c.ID, nil, &a.ID); err != nil {
		t.Errorf("legal move: %v", err)
	}
}

func TestFolderRenameCollision(t *testing.T) {
	s, _ := newTestStore(t)
	a, _ := s.CreateFolder("a", nil)
	b, _ := s.CreateFolder("b", nil)
	if _, err := s.UpdateFolder(a.ID, strPtr("B"), nil); !errors.Is(err, ErrNameTaken) {
		t.Errorf("expected ErrNameTaken, got %v", err)
	}
	if _, err := s.UpdateFolder(b.ID, strPtr("b"), nil); err != nil {
		t.Errorf("rename to own name should be ok: %v", err)
	}
}

func TestFolderDeleteRefusal(t *testing.T) {
	s, _ := newTestStore(t)
	f, _ := s.CreateFolder("f", nil)
	sn, _ := s.CreateSnippet(SnippetInput{Title: "x", Body: "1", FolderID: &f.ID})

	if err := s.DeleteFolder(f.ID); !errors.Is(err, ErrFolderNotEmpty) {
		t.Errorf("expected not empty (snippets), got %v", err)
	}
	child, _ := s.CreateFolder("child", &f.ID)
	if err := s.DeleteFolder(f.ID); !errors.Is(err, ErrFolderNotEmpty) {
		t.Errorf("expected not empty (children), got %v", err)
	}

	if err := s.SoftDeleteSnippet(sn.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteFolder(child.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteFolder(f.ID); err != nil {
		t.Errorf("expected ok, got %v", err)
	}
	list, _ := s.ListFolders()
	if len(list) != 0 {
		t.Errorf("folders after delete = %d", len(list))
	}
	if err := s.DeleteFolder(f.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete of deleted folder: %v", err)
	}
}

// TestFolderMoveDestinationLive: the move destination must be a live
// folder. Moving under a soft-deleted or missing folder used to succeed,
// leaving a live folder orphaned under a tombstone (and later blocking
// the daily purge with a foreign-key error).
func TestFolderMoveDestinationLive(t *testing.T) {
	s, _ := newTestStore(t)
	p, _ := s.CreateFolder("p", nil)
	c, _ := s.CreateFolder("c", &p.ID)
	x, _ := s.CreateFolder("x", nil)

	// Empty p (delete its child), delete p, then try to move the live x
	// under the tombstoned p.
	if err := s.DeleteFolder(c.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteFolder(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateFolder(x.ID, nil, &p.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("move under soft-deleted parent: got %v, want ErrNotFound", err)
	}
	missing := "01JXXXXXXXXXXXXXXXXXXXXXXXXXX"
	if _, err := s.UpdateFolder(x.ID, nil, &missing); !errors.Is(err, ErrNotFound) {
		t.Errorf("move under missing parent: got %v, want ErrNotFound", err)
	}
	// x is unharmed and still movable to a live parent.
	a, _ := s.CreateFolder("a", nil)
	if _, err := s.UpdateFolder(x.ID, nil, &a.ID); err != nil {
		t.Errorf("legal move after refusals: %v", err)
	}
}

// TestFolderMoveSiblingCollision: moving (without renaming) into a
// parent that already has a child with this folder's name is refused —
// the move previously skipped the sibling-uniqueness check and could
// create duplicate siblings.
func TestFolderMoveSiblingCollision(t *testing.T) {
	s, _ := newTestStore(t)
	a, _ := s.CreateFolder("a", nil)
	dupRoot, _ := s.CreateFolder("dup", nil)
	dupChild, err := s.CreateFolder("dup", &a.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Moving the root-level "dup" under a collides with a's child "dup".
	if _, err := s.UpdateFolder(dupRoot.ID, nil, &a.ID); !errors.Is(err, ErrNameTaken) {
		t.Errorf("move collision: got %v, want ErrNameTaken", err)
	}
	// The same move succeeds once the sibling is renamed away.
	if _, err := s.UpdateFolder(dupChild.ID, strPtr("gone"), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateFolder(dupRoot.ID, nil, &a.ID); err != nil {
		t.Errorf("move after clearing collision: %v", err)
	}
}
