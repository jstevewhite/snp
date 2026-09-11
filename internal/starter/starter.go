// Package starter holds snp's pre-packaged starter snippets (spec §5): a
// small curated pack embedded in the binary and applied on demand.
//
// The pack is an ordinary import document (version 1), so seeding is the
// existing, already-tested import path — no new format and no new store
// code. It is applied with merge mode against pinned ids, which makes a
// second apply an update rather than a duplicate.
//
// Nothing seeds itself. Import's merge mode clears deleted_at, so an
// automatic apply would resurrect snippets the user had deleted; the pack
// is only ever written on an explicit request — `snp seed`,
// POST /api/seed, or the SPA's settings panel.
package starter

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/jstevewhite/snp/internal/store"
)

//go:embed pack.json
var packJSON []byte

// Pack returns the starter import document. It is unmarshalled on every
// call, so a caller cannot mutate a document another caller is holding;
// the pack is a few kB.
func Pack() (store.ImportDoc, error) {
	var doc store.ImportDoc
	if err := json.Unmarshal(packJSON, &doc); err != nil {
		return store.ImportDoc{}, fmt.Errorf("starter: parse pack: %w", err)
	}
	return doc, nil
}
