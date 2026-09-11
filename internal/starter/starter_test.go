package starter

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jstevewhite/snp/internal/store"
)

// ulidRe matches a ULID (26 Crockford base32 characters). The pack pins its
// ids deliberately: merge upserts by id, so a stable id means re-seeding
// updates the existing row instead of adding a duplicate.
var ulidRe = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)

func TestPackIsWellFormed(t *testing.T) {
	doc, err := Pack()
	if err != nil {
		t.Fatal(err)
	}
	if doc.Version != 1 {
		t.Errorf("version = %d, want 1", doc.Version)
	}
	if len(doc.Snippets) == 0 {
		t.Fatal("pack has no snippets")
	}
	seen := map[string]bool{}
	for _, sn := range doc.Snippets {
		if !ulidRe.MatchString(sn.ID) {
			t.Errorf("%q: id %q is not a ULID", sn.Title, sn.ID)
		}
		if seen[sn.ID] {
			t.Errorf("%q: duplicate id %q", sn.Title, sn.ID)
		}
		seen[sn.ID] = true
		if strings.TrimSpace(sn.Title) == "" || strings.TrimSpace(sn.Body) == "" {
			t.Errorf("%q: empty title or body", sn.ID)
		}
		if sn.IsSensitive {
			t.Errorf("%q: pack snippets must not be sensitive", sn.Title)
		}
		// The flag drives the variables panel; import must not set it for
		// a body with no placeholders (spec §4).
		if sn.UsesVariables != strings.Contains(sn.Body, "{{") {
			t.Errorf("%q: uses_variables=%v does not match the body", sn.Title, sn.UsesVariables)
		}
	}
}

func TestPackShipsTheConfigFileSnippet(t *testing.T) {
	// The snippet the pack exists for: a copy-pasteable snp config.toml.
	// Pinned so a later edit cannot quietly drop it.
	doc, err := Pack()
	if err != nil {
		t.Fatal(err)
	}
	for _, sn := range doc.Snippets {
		if sn.Title == "snp config.toml" {
			if sn.Language != "toml" || !strings.Contains(sn.Body, "ai_key") ||
				!strings.Contains(sn.Body, "state_dir") {
				t.Errorf("config snippet = %+v", sn)
			}
			return
		}
	}
	t.Error("pack does not contain the snp config.toml snippet")
}

func TestPackAppliesIdempotently(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "snp.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	key, err := store.LoadOrCreateKey(filepath.Join(t.TempDir(), "key"))
	if err != nil {
		t.Fatal(err)
	}
	st.SetKey(key)

	doc, err := Pack()
	if err != nil {
		t.Fatal(err)
	}
	first, err := st.Import(doc, "merge")
	if err != nil {
		t.Fatal(err)
	}
	if first.Created != len(doc.Snippets) || first.Updated != 0 {
		t.Errorf("first apply = %+v, want %d created / 0 updated", first, len(doc.Snippets))
	}
	// Applying again must update in place, not duplicate: this is what
	// makes "Add starter snippets" safe to press twice.
	second, err := st.Import(doc, "merge")
	if err != nil {
		t.Fatal(err)
	}
	if second.Created != 0 || second.Updated != len(doc.Snippets) {
		t.Errorf("second apply = %+v, want 0 created / %d updated", second, len(doc.Snippets))
	}

	exp, err := st.Export()
	if err != nil {
		t.Fatal(err)
	}
	if len(exp.Snippets) != len(doc.Snippets) {
		t.Errorf("store holds %d snippets, want %d", len(exp.Snippets), len(doc.Snippets))
	}
	// Every snippet names folder_path "Starter", which import creates.
	found := false
	for _, f := range exp.Folders {
		if f.Name == "Starter" {
			found = true
		}
	}
	if !found {
		t.Error("folder_path did not create the Starter folder")
	}
}
