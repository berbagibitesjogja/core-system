-- +goose Up
CREATE TABLE reimbursements (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    amount BIGINT NOT NULL CHECK (amount > 0),
    method VARCHAR(50) NOT NULL,
    target VARCHAR(100) NOT NULL,
    notes TEXT,
    status VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'completed', 'rejected')),
    payment_proof UUID REFERENCES files(id) ON DELETE SET NULL,
    paid_by UUID REFERENCES users(id) ON DELETE SET NULL,
    paid_at TIMESTAMPTZ,
    rejection_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);

CREATE INDEX idx_reimbursements_user_id ON reimbursements (user_id);
CREATE INDEX idx_reimbursements_status ON reimbursements (status) WHERE deleted_at IS NULL;
CREATE INDEX idx_reimbursements_created_at ON reimbursements (created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS reimbursements;
