-- +goose Up

-- Default seeder password: 'password'
INSERT INTO users (id, email, password_hash, name, phone_number, is_active, email_verified_at) VALUES
    ('00000000-0000-0000-0000-000000000001', 'it@berbagibitesjogja.com', '$2a$10$tS9YBF9cULWafDypXzP1ZOEttaMQMCIL29gC7gzfXXAfiY5vcNzZS', 'Tim IT', '6200000000001', true, NOW()),
    ('00000000-0000-0000-0000-000000000002', 'mg.sp@berbagibitesjogja.com', '$2a$10$tS9YBF9cULWafDypXzP1ZOEttaMQMCIL29gC7gzfXXAfiY5vcNzZS', 'Managerial Super', '6200000000002', true, NOW()),
    ('00000000-0000-0000-0000-000000000003', 'mg.c@berbagibitesjogja.com', '$2a$10$tS9YBF9cULWafDypXzP1ZOEttaMQMCIL29gC7gzfXXAfiY5vcNzZS', 'Managerial Core', '6200000000003', true, NOW()),
    ('00000000-0000-0000-0000-000000000004', 'mg.sf@berbagibitesjogja.com', '$2a$10$tS9YBF9cULWafDypXzP1ZOEttaMQMCIL29gC7gzfXXAfiY5vcNzZS', 'Managerial Staff', '6200000000004', true, NOW()),
    ('00000000-0000-0000-0000-000000000005', 'op.sp@berbagibitesjogja.com', '$2a$10$tS9YBF9cULWafDypXzP1ZOEttaMQMCIL29gC7gzfXXAfiY5vcNzZS', 'Operational Super', '6200000000005', true, NOW()),
    ('00000000-0000-0000-0000-000000000006', 'op.c@berbagibitesjogja.com', '$2a$10$tS9YBF9cULWafDypXzP1ZOEttaMQMCIL29gC7gzfXXAfiY5vcNzZS', 'Operational Core', '6200000000006', true, NOW()),
    ('00000000-0000-0000-0000-000000000007', 'op.sf@berbagibitesjogja.com', '$2a$10$tS9YBF9cULWafDypXzP1ZOEttaMQMCIL29gC7gzfXXAfiY5vcNzZS', 'Operational Staff', '6200000000007', true, NOW()),
    ('00000000-0000-0000-0000-000000000008', 'medinfo.sp@berbagibitesjogja.com', '$2a$10$tS9YBF9cULWafDypXzP1ZOEttaMQMCIL29gC7gzfXXAfiY5vcNzZS', 'Medinfo Super', '6200000000008', true, NOW()),
    ('00000000-0000-0000-0000-000000000009', 'medinfo.c@berbagibitesjogja.com', '$2a$10$tS9YBF9cULWafDypXzP1ZOEttaMQMCIL29gC7gzfXXAfiY5vcNzZS', 'Medinfo Core', '6200000000009', true, NOW()),
    ('00000000-0000-0000-0000-000000000010', 'medinfo.sf@berbagibitesjogja.com', '$2a$10$tS9YBF9cULWafDypXzP1ZOEttaMQMCIL29gC7gzfXXAfiY5vcNzZS', 'Medinfo Staff', '6200000000010', true, NOW()),
    ('00000000-0000-0000-0000-000000000101', 'hero.ugm@mail.ugm.ac.id', '$2a$10$tS9YBF9cULWafDypXzP1ZOEttaMQMCIL29gC7gzfXXAfiY5vcNzZS', 'Hero Mahasiswa UGM', '6200000000101', true, NOW()),
    ('00000000-0000-0000-0000-000000000102', 'hero.public@gmail.com', '$2a$10$tS9YBF9cULWafDypXzP1ZOEttaMQMCIL29gC7gzfXXAfiY5vcNzZS', 'Hero Masyarakat Umum', '6200000000102', true, NULL);

INSERT INTO staff (user_id, division_id, role) VALUES
    ('00000000-0000-0000-0000-000000000001', 1, 'super'),
    ('00000000-0000-0000-0000-000000000002', 2, 'super'),
    ('00000000-0000-0000-0000-000000000003', 2, 'core'),
    ('00000000-0000-0000-0000-000000000004', 2, 'staff'),
    ('00000000-0000-0000-0000-000000000005', 3, 'super'),
    ('00000000-0000-0000-0000-000000000006', 3, 'core'),
    ('00000000-0000-0000-0000-000000000007', 3, 'staff'),
    ('00000000-0000-0000-0000-000000000008', 4, 'super'),
    ('00000000-0000-0000-0000-000000000009', 4, 'core'),
    ('00000000-0000-0000-0000-000000000010', 4, 'staff');

INSERT INTO heroes (user_id, remind_me_at) VALUES
    ('00000000-0000-0000-0000-000000000101', NULL),
    ('00000000-0000-0000-0000-000000000102', NULL);

-- +goose Down
TRUNCATE TABLE users CASCADE;
