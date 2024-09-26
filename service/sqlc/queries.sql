-- name: InsertMetadata :exec
INSERT INTO metadata (id)
VALUES (@id);

-- name: GetMetadataByIDs :many
SELECT id
FROM metadata
WHERE id = ANY(@ids::VARCHAR[]);