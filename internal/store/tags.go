package store

import (
	"context"
)

// TagCount is a tag name with its live snippet count.
type TagCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// ListTags lists live tags with counts, most-used first.
func (s *Store) ListTags() ([]TagCount, error) {
	ctx := context.Background()
	rows, err := s.db.QueryContext(ctx, `
		SELECT tg.name, COUNT(*) AS n
		FROM tags tg
		JOIN snippet_tags st ON st.tag_id = tg.id
		JOIN snippets sn ON sn.id = st.snippet_id AND sn.deleted_at IS NULL
		GROUP BY tg.id
		ORDER BY n DESC, tg.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TagCount{}
	for rows.Next() {
		var tc TagCount
		if err := rows.Scan(&tc.Name, &tc.Count); err != nil {
			return nil, err
		}
		out = append(out, tc)
	}
	return out, rows.Err()
}
