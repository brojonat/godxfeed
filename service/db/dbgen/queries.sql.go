// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.26.0
// source: queries.sql

package dbgen

import (
	"context"
)

const getMetadataByIDs = `-- name: GetMetadataByIDs :many
SELECT id
FROM metadata
WHERE id = ANY($1::VARCHAR[])
`

func (q *Queries) GetMetadataByIDs(ctx context.Context, ids []string) ([]string, error) {
	rows, err := q.db.Query(ctx, getMetadataByIDs, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		items = append(items, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const insertMetadata = `-- name: InsertMetadata :exec
INSERT INTO metadata (id)
VALUES ($1)
`

func (q *Queries) InsertMetadata(ctx context.Context, id string) error {
	_, err := q.db.Exec(ctx, insertMetadata, id)
	return err
}
