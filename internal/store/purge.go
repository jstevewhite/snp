package store

import (
	"context"
	"log/slog"
	"time"
)

// PurgeCounts reports what Purge removed.
type PurgeCounts struct {
	Snippets int
	Folders  int
	Tags     int
}

// Purge hard-deletes rows soft-deleted more than 30 days ago and prunes
// unreferenced tags (spec §4).
func (s *Store) Purge() (PurgeCounts, error) {
	ctx := context.Background()
	cutoff := s.clock.Now().UTC().Add(-30 * 24 * time.Hour).Truncate(time.Second).Format("2006-01-02T15:04:05Z")

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PurgeCounts{}, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx,
		`SELECT rowid, id FROM snippets WHERE deleted_at IS NOT NULL AND deleted_at <= ?`, cutoff)
	if err != nil {
		return PurgeCounts{}, err
	}
	type rid struct {
		rowid int64
		id    string
	}
	var pending []rid
	for rows.Next() {
		var r rid
		if err := rows.Scan(&r.rowid, &r.id); err != nil {
			rows.Close()
			return PurgeCounts{}, err
		}
		pending = append(pending, r)
	}
	rows.Close()
	// FTS row and tag rows go before the main row (spec §4).
	for _, r := range pending {
		if _, err := tx.ExecContext(ctx, `DELETE FROM snippet_tags WHERE snippet_id = ?`, r.id); err != nil {
			return PurgeCounts{}, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM snippets_fts WHERE rowid = ?`, r.rowid); err != nil {
			return PurgeCounts{}, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM snippets WHERE id = ?`, r.id); err != nil {
			return PurgeCounts{}, err
		}
	}

	var c PurgeCounts
	c.Snippets = len(pending)

	// Recent snippet/folder tombstones may still reference an older folder.
	// Defer that folder until its dependents have been purged or restored.
	res, err := tx.ExecContext(ctx,
		`DELETE FROM folders WHERE deleted_at IS NOT NULL AND deleted_at <= ?
         AND id NOT IN (SELECT folder_id FROM snippets WHERE folder_id IS NOT NULL)
         AND id NOT IN (SELECT parent_id FROM folders WHERE parent_id IS NOT NULL)`, cutoff)
	if err != nil {
		return PurgeCounts{}, err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		c.Folders = int(n)
	}

	res, err = tx.ExecContext(ctx,
		`DELETE FROM tags WHERE id NOT IN (SELECT DISTINCT tag_id FROM snippet_tags)`)
	if err != nil {
		return PurgeCounts{}, err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		c.Tags = int(n)
	}

	if err := tx.Commit(); err != nil {
		return PurgeCounts{}, err
	}
	return c, nil
}

// StartPurger runs Purge immediately and every 24h until ctx is done
// (spec §4). Logging goes through log, the server's configured logger,
// not the package default. The returned channel is closed when the
// goroutine has stopped; serve waits on it at shutdown so the database
// is never closed underneath a running purge.
func (s *Store) StartPurger(ctx context.Context, log *slog.Logger) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		run := func() {
			c, err := s.Purge()
			if err != nil {
				log.Error("purge failed", "err", err)
				return
			}
			if c.Snippets > 0 || c.Folders > 0 || c.Tags > 0 {
				log.Info("purge complete", "snippets", c.Snippets, "folders", c.Folders, "tags", c.Tags)
			}
		}
		run()
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()
	return done
}
