-- +goose Up

-- Default seeder password: 'password'
INSERT INTO users (id, division_id, email, password_hash, role, name, phone_number) VALUES
    ('00000000-0000-0000-0000-000000000001', 1, 'it@berbagibitesjogja.com', '$2a$10$tS9YBF9cULWafDypXzP1ZOEttaMQMCIL29gC7gzfXXAfiY5vcNzZS', 'super', 'Tim IT', '6200000000001'),
    ('00000000-0000-0000-0000-000000000002', 2, 'mg.sp@berbagibitesjogja.com', '$2a$10$tS9YBF9cULWafDypXzP1ZOEttaMQMCIL29gC7gzfXXAfiY5vcNzZS', 'super', 'Managerial Super', '6200000000002'),
    ('00000000-0000-0000-0000-000000000003', 2, 'mg.c@berbagibitesjogja.com', '$2a$10$tS9YBF9cULWafDypXzP1ZOEttaMQMCIL29gC7gzfXXAfiY5vcNzZS', 'core', 'Managerial Core', '6200000000003'),
    ('00000000-0000-0000-0000-000000000004', 2, 'mg.sf@berbagibitesjogja.com', '$2a$10$tS9YBF9cULWafDypXzP1ZOEttaMQMCIL29gC7gzfXXAfiY5vcNzZS', 'staff', 'Managerial Staff', '6200000000004'),
    ('00000000-0000-0000-0000-000000000005', 3, 'op.sp@berbagibitesjogja.com', '$2a$10$tS9YBF9cULWafDypXzP1ZOEttaMQMCIL29gC7gzfXXAfiY5vcNzZS', 'super', 'Operational Super', '6200000000005'),
    ('00000000-0000-0000-0000-000000000006', 3, 'op.c@berbagibitesjogja.com', '$2a$10$tS9YBF9cULWafDypXzP1ZOEttaMQMCIL29gC7gzfXXAfiY5vcNzZS', 'core', 'Operational Core', '6200000000006'),
    ('00000000-0000-0000-0000-000000000007', 3, 'op.sf@berbagibitesjogja.com', '$2a$10$tS9YBF9cULWafDypXzP1ZOEttaMQMCIL29gC7gzfXXAfiY5vcNzZS', 'staff', 'Operational Staff', '6200000000007'),
    ('00000000-0000-0000-0000-000000000008', 4, 'medinfo.sp@berbagibitesjogja.com', '$2a$10$tS9YBF9cULWafDypXzP1ZOEttaMQMCIL29gC7gzfXXAfiY5vcNzZS', 'super', 'Medinfo Super', '6200000000008'),
    ('00000000-0000-0000-0000-000000000009', 4, 'medinfo.c@berbagibitesjogja.com', '$2a$10$tS9YBF9cULWafDypXzP1ZOEttaMQMCIL29gC7gzfXXAfiY5vcNzZS', 'core', 'Medinfo Core', '6200000000009'),
    ('00000000-0000-0000-0000-000000000010', 4, 'medinfo.sf@berbagibitesjogja.com', '$2a$10$tS9YBF9cULWafDypXzP1ZOEttaMQMCIL29gC7gzfXXAfiY5vcNzZS', 'staff', 'Medinfo Staff', '6200000000010');

-- +goose Down
DELETE FROM users;
