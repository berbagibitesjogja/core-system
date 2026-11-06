-- +goose Up
CREATE TABLE reimbursement_files (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    reimbursement_id UUID NOT NULL REFERENCES reimbursements(id) ON DELETE CASCADE,
    file_id UUID NOT NULL REFERENCES files(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_reimbursement_files_pair UNIQUE (reimbursement_id, file_id)
);

CREATE INDEX idx_reimbursement_files_reimbursement_id ON reimbursement_files (reimbursement_id);
CREATE INDEX idx_reimbursement_files_file_id ON reimbursement_files (file_id);

-- +goose Down
DROP TABLE IF EXISTS reimbursement_files;
