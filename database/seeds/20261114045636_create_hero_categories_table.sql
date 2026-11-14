-- +goose Up

INSERT INTO hero_categories (hero_id, beneficiary_category_id)
SELECT v.hero_id, bc.id
FROM (
    VALUES
        ('00000000-0000-0000-0000-000000000101'::uuid, 'Universitas Gadjah Mada'),
        ('00000000-0000-0000-0000-000000000102'::uuid, 'Masyarakat Umum')
) AS v(hero_id, category_name)
JOIN beneficiary_categories bc ON bc.name = v.category_name;

-- +goose Down
TRUNCATE TABLE hero_categories CASCADE;
