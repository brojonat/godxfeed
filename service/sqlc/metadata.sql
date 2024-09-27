-- name: InsertMetadata :exec
INSERT INTO metadata (symbol, data)
VALUES (@symbol, @data)
ON CONFLICT ON CONSTRAINT metadata_pkey DO UPDATE
SET data = EXCLUDED.data;

-- name: GetMetadataByIDs :many
SELECT symbol, data
FROM metadata
WHERE symbol = ANY(@symbols::VARCHAR[]);

