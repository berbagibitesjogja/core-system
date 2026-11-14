-- name: GetFileByID :one
SELECT id, storage_bucket, storage_key, status, mime_type
FROM files
WHERE id = $1
LIMIT 1;
