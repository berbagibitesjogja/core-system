-- +goose Up

CREATE TYPE file_upload_status AS ENUM ('uploading', 'uploaded', 'cancelled');

CREATE TABLE files (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  storage_bucket VARCHAR(255) NOT NULL,
  storage_key VARCHAR(512) NOT NULL,
  status file_upload_status NOT NULL DEFAULT 'uploading',
  original_name VARCHAR(255),
  mime_type VARCHAR(100) NOT NULL,
  size_bytes bigint NOT NULL DEFAULT 0,
  hash_sha256 CHAR(64),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT uq_files_bucket_key UNIQUE (storage_bucket, storage_key)
);

CREATE INDEX idx_files_hash_sha256 ON files (hash_sha256);

CREATE INDEX idx_files_status ON files (status);

-- +goose Down
DROP TABLE IF EXISTS files;

DROP TYPE IF EXISTS file_upload_status;
