package store

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

// TestVarDefaultsCRUD covers the non-sensitive round trip: create, read,
// list, replace, and replacing with an empty map (spec §4).
func TestVarDefaultsCRUD(t *testing.T) {
	s, _ := newTestStore(t)

	created := mustCreate(t, s, SnippetInput{
		Title: "deploy", Body: "kubectl -n {{ns}} set image {{app}}",
		UsesVariables: true,
		VarDefaults:   map[string]string{"ns": "prod", "app": "snp"},
	})
	if created.VarDefaults["ns"] != "prod" || created.VarDefaults["app"] != "snp" {
		t.Errorf("create out defaults: %+v", created.VarDefaults)
	}

	got, err := s.GetSnippet(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.VarDefaults["ns"] != "prod" || got.VarDefaults["app"] != "snp" {
		t.Errorf("get defaults: %+v", got.VarDefaults)
	}

	list, err := s.ListSnippets(ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].VarDefaults["ns"] != "prod" {
		t.Errorf("list defaults: %+v", list)
	}

	// Replace with a pruned map: the client drops keys for variables that
	// are no longer in the body; the store carries what it is given.
	updated, err := s.ReplaceSnippet(created.ID, SnippetInput{
		Title: "deploy", Body: "kubectl -n {{ns}} get pods",
		UsesVariables: true,
		VarDefaults:   map[string]string{"ns": "staging"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.VarDefaults) != 1 || updated.VarDefaults["ns"] != "staging" {
		t.Errorf("replace defaults: %+v", updated.VarDefaults)
	}

	// Replacing with no defaults yields {} (not null) for JSON consumers.
	empty, err := s.ReplaceSnippet(created.ID, SnippetInput{
		Title: "deploy", Body: "kubectl get pods",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.VarDefaults) != 0 {
		t.Errorf("empty replace defaults: %+v", empty.VarDefaults)
	}
	raw, err := json.Marshal(empty.VarDefaults)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "{}" {
		t.Errorf("empty map JSON: %s", raw)
	}
}

// TestVarDefaultsSensitive checks that defaults for sensitive snippets are
// sealed with the key (spec §4 "Encryption") and hidden in create output.
func TestVarDefaultsSensitive(t *testing.T) {
	s, _ := newTestStore(t)

	created, err := s.CreateSnippet(SnippetInput{
		Title: "token", Body: "curl -H 'Authorization: Bearer {{token}}'",
		IsSensitive:   true,
		UsesVariables: true,
		VarDefaults:   map[string]string{"token": "s3cret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Sensitive output hides the body and the defaults (spec §5).
	if created.Body != nil || created.VarDefaults != nil {
		t.Errorf("sensitive create output leaked: body=%v defaults=%v", created.Body, created.VarDefaults)
	}

	got, err := s.GetSnippet(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Body == nil || *got.Body != "curl -H 'Authorization: Bearer {{token}}'" {
		t.Errorf("body not decrypted: %v", got.Body)
	}
	if got.VarDefaults["token"] != "s3cret" {
		t.Errorf("get defaults: %+v", got.VarDefaults)
	}

	// At rest: the plain column is empty and the encrypted column holds the
	// sealed map (never the plaintext JSON).
	var vdPlain string
	var vdEnc []byte
	if err := s.db.QueryRow(`SELECT var_defaults, var_defaults_enc FROM snippets WHERE id = ?`, created.ID).
		Scan(&vdPlain, &vdEnc); err != nil {
		t.Fatal(err)
	}
	if vdPlain != "" {
		t.Errorf("plain column for sensitive row: %q", vdPlain)
	}
	if len(vdEnc) == 0 {
		t.Error("no ciphertext for sensitive defaults")
	}
	if containsStr(string(vdEnc), "s3cret") {
		t.Error("plaintext default visible in the stored ciphertext blob")
	}

	// Sensitive snippets with no defaults keep the ciphertext column NULL.
	plain := mustCreate(t, s, SnippetInput{Title: "p", Body: "x", IsSensitive: true})
	var encNull bool
	if err := s.db.QueryRow(`SELECT var_defaults_enc IS NULL FROM snippets WHERE id = ?`, plain.ID).
		Scan(&encNull); err != nil {
		t.Fatal(err)
	}
	if !encNull {
		t.Error("var_defaults_enc should be NULL with no defaults")
	}
}

// containsStr reports whether s contains sub (test helper, byte-wise).
func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestVarDefaultsSensitiveToggle covers toggling is_sensitive via replace:
// the defaults survive in both directions and the ciphertext column is
// managed with the flag (spec §4 "Encryption").
func TestVarDefaultsSensitiveToggle(t *testing.T) {
	s, _ := newTestStore(t)
	created := mustCreate(t, s, SnippetInput{
		Title: "toggle", Body: "ping {{host}}",
		UsesVariables: true,
		VarDefaults:   map[string]string{"host": "example.com"},
	})

	encState := func(t *testing.T, id string) bool {
		t.Helper()
		var encNull bool
		if err := s.db.QueryRow(`SELECT var_defaults_enc IS NULL FROM snippets WHERE id = ?`, id).
			Scan(&encNull); err != nil {
			t.Fatal(err)
		}
		return encNull
	}
	if !encState(t, created.ID) {
		t.Error("non-sensitive row should have NULL ciphertext")
	}

	// Non-sensitive -> sensitive: the map must survive, now sealed.
	sens, err := s.ReplaceSnippet(created.ID, SnippetInput{
		Title: "toggle", Body: "ping {{host}}",
		IsSensitive:   true,
		UsesVariables: true,
		VarDefaults:   map[string]string{"host": "example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if sens.VarDefaults != nil {
		t.Errorf("sensitive replace output leaked defaults: %+v", sens.VarDefaults)
	}
	if encState(t, created.ID) {
		t.Error("ciphertext column not populated after toggle to sensitive")
	}
	got, err := s.GetSnippet(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.VarDefaults["host"] != "example.com" {
		t.Errorf("defaults lost on toggle: %+v", got.VarDefaults)
	}

	// Sensitive -> non-sensitive: the map comes back, ciphertext dropped.
	plain, err := s.ReplaceSnippet(created.ID, SnippetInput{
		Title: "toggle", Body: "ping {{host}}",
		UsesVariables: true,
		VarDefaults:   map[string]string{"host": "example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plain.VarDefaults["host"] != "example.com" {
		t.Errorf("defaults lost on toggle back: %+v", plain.VarDefaults)
	}
	if !encState(t, created.ID) {
		t.Error("ciphertext column not cleared after toggle to non-sensitive")
	}
}

// TestVarDefaultsNoKey: sensitive snippets require the key for the body, so
// creating one without a key must fail with ErrNoKey (defaults included);
// non-sensitive snippets with defaults need no key at all.
func TestVarDefaultsNoKey(t *testing.T) {
	p := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.SetClock(&fakeClock{t: baseTime})

	if _, err := s.CreateSnippet(SnippetInput{
		Title: "plain", Body: "x {{y}}", VarDefaults: map[string]string{"y": "z"},
	}); err != nil {
		t.Errorf("non-sensitive without key: %v", err)
	}
	if _, err := s.CreateSnippet(SnippetInput{
		Title: "secret", Body: "x {{y}}", IsSensitive: true,
		VarDefaults: map[string]string{"y": "z"},
	}); !errors.Is(err, ErrNoKey) {
		t.Errorf("expected ErrNoKey, got %v", err)
	}
}

// TestSyncCarriesVarDefaults: live snippets carry their defaults in sync;
// sensitive snippets hide them (JSON null), like their body.
func TestSyncCarriesVarDefaults(t *testing.T) {
	s, _ := newTestStore(t)
	mustCreate(t, s, SnippetInput{
		Title: "plain", Body: "a {{b}}",
		UsesVariables: true,
		VarDefaults:   map[string]string{"b": "2"},
	})
	mustCreate(t, s, SnippetInput{
		Title: "secret", Body: "c {{d}}",
		IsSensitive:   true,
		UsesVariables: true,
		VarDefaults:   map[string]string{"d": "4"},
	})

	out, err := s.SyncSince("")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Snippets) != 2 {
		t.Fatalf("sync snippets: %v", out.Snippets)
	}
	var plain, secret SnippetOut
	for _, item := range out.Snippets {
		sn, ok := item.(SnippetOut)
		if !ok {
			continue
		}
		if sn.IsSensitive {
			secret = sn
		} else {
			plain = sn
		}
	}
	if plain.VarDefaults["b"] != "2" {
		t.Errorf("sync plain defaults: %+v", plain.VarDefaults)
	}
	if secret.VarDefaults != nil {
		t.Errorf("sync sensitive defaults must be null, got %+v", secret.VarDefaults)
	}
	blob, err := json.Marshal(secret)
	if err != nil {
		t.Fatal(err)
	}
	if !containsStr(string(blob), `"var_defaults":null`) {
		t.Errorf("sensitive sync JSON: %s", blob)
	}
}

// TestExportImportVarDefaults: exports carry decrypted defaults; imports
// recreate them, re-encrypting sensitive rows under the new key.
func TestExportImportVarDefaults(t *testing.T) {
	s, _ := newTestStore(t)
	mustCreate(t, s, SnippetInput{
		Title: "plain", Body: "a {{b}}",
		UsesVariables: true,
		VarDefaults:   map[string]string{"b": "2"},
	})
	secretID := mustCreate(t, s, SnippetInput{
		Title: "secret", Body: "c {{d}}",
		IsSensitive:   true,
		UsesVariables: true,
		VarDefaults:   map[string]string{"d": "4"},
	}).ID

	doc, err := s.Export()
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Snippets) != 2 {
		t.Fatalf("export snippets: %d", len(doc.Snippets))
	}
	for _, sn := range doc.Snippets {
		switch sn.ID {
		case secretID:
			if sn.VarDefaults["d"] != "4" {
				t.Errorf("export sensitive defaults not decrypted: %+v", sn.VarDefaults)
			}
		default:
			if sn.VarDefaults["b"] != "2" {
				t.Errorf("export plain defaults: %+v", sn.VarDefaults)
			}
		}
	}

	// Import into a fresh store (fresh key): defaults survive, sensitive
	// rows are re-encrypted under the new key.
	p := filepath.Join(t.TempDir(), "fresh.db")
	fresh, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fresh.Close() })
	fc := &fakeClock{t: baseTime}
	fresh.SetClock(fc)
	fresh.SetKey(newKeyForTest(t))
	imp, err := jsonMarshalImportDoc(doc)
	if err != nil {
		t.Fatal(err)
	}
	res, err := fresh.Import(imp, "replace")
	if err != nil {
		t.Fatal(err)
	}
	if res.Created != 2 {
		t.Errorf("import created: %d", res.Created)
	}
	got, err := fresh.GetSnippet(secretID)
	if err != nil {
		t.Fatal(err)
	}
	if got.VarDefaults["d"] != "4" {
		t.Errorf("imported sensitive defaults: %+v", got.VarDefaults)
	}
	var encNull bool
	if err := fresh.db.QueryRow(`SELECT var_defaults_enc IS NULL FROM snippets WHERE id = ?`, secretID).
		Scan(&encNull); err != nil {
		t.Fatal(err)
	}
	if encNull {
		t.Error("imported sensitive row should have sealed defaults")
	}
}

// jsonMarshalImportDoc maps an export document to the import shape.
func jsonMarshalImportDoc(doc ExportDoc) (ImportDoc, error) {
	var imp ImportDoc
	blob, err := json.Marshal(doc)
	if err != nil {
		return imp, err
	}
	if err := json.Unmarshal(blob, &imp); err != nil {
		return imp, err
	}
	return imp, nil
}

// TestUsesVariablesInOut: create and replace responses carry the template
// flag directly from the input; the GET/list/sync paths read it back from
// the row. The responses build SnippetOut without a round trip, so the
// field must be set explicitly or the client cache (seeded from the
// response) loses it.
func TestUsesVariablesInOut(t *testing.T) {
	s, _ := newTestStore(t)
	created := mustCreate(t, s, SnippetInput{
		Title: "tpl", Body: "x {{y}}", UsesVariables: true,
	})
	if !created.UsesVariables {
		t.Error("create out should carry UsesVariables")
	}
	updated, err := s.ReplaceSnippet(created.ID, SnippetInput{
		Title: "tpl", Body: "x {{y}}", UsesVariables: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.UsesVariables {
		t.Error("replace out should carry UsesVariables")
	}
	plain, err := s.ReplaceSnippet(created.ID, SnippetInput{
		Title: "tpl", Body: "plain",
	})
	if err != nil {
		t.Fatal(err)
	}
	if plain.UsesVariables {
		t.Error("replace out should carry the cleared flag")
	}
}

// TestImportOldDocWithoutVarDefaults: export docs without the field (pre-
// 0003) import as snippets with empty default maps.
func TestImportOldDocWithoutVarDefaults(t *testing.T) {
	s, _ := newTestStore(t)
	const docJSON = `{
		"version": 1,
		"snippets": [{
			"id": "old-0001",
			"title": "old export",
			"body": "run {{cmd}}",
			"uses_variables": true,
			"created_at": "2026-09-02T10:00:00Z",
			"updated_at": "2026-09-02T10:00:00Z"
		}]
	}`
	var imp ImportDoc
	if err := json.Unmarshal([]byte(docJSON), &imp); err != nil {
		t.Fatal(err)
	}
	res, err := s.Import(imp, "merge")
	if err != nil {
		t.Fatal(err)
	}
	if res.Created != 1 {
		t.Fatalf("created: %d", res.Created)
	}
	if len(imp.Snippets[0].VarDefaults) != 0 {
		t.Errorf("missing field should decode as empty map: %+v", imp.Snippets[0].VarDefaults)
	}
	got, err := s.GetSnippet(imp.Snippets[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.VarDefaults) != 0 {
		t.Errorf("imported defaults: %+v", got.VarDefaults)
	}
}

// newKeyForTest returns a throwaway encryption key for fresh stores.
func newKeyForTest(t *testing.T) *Key {
	t.Helper()
	k, err := LoadOrCreateKey(filepath.Join(t.TempDir(), "key"))
	if err != nil {
		t.Fatalf("LoadOrCreateKey: %v", err)
	}
	return k
}
