-- +goose Up
INSERT INTO divisions (id, name, created_at, updated_at) VALUES
(1, 'IT', NOW(), NOW()),
(2, 'Managerial', NOW(), NOW()),
(3, 'Operational', NOW(), NOW()),
(4, 'Medinfo', NOW(), NOW());

SELECT setval('divisions_id_seq', (SELECT MAX(id) FROM divisions));

-- +goose Down
DELETE FROM divisions WHERE id IN (1, 2, 3, 4);
