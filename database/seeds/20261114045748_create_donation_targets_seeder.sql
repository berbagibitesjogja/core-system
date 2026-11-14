-- +goose Up

INSERT INTO donation_target (donation_id, beneficiary_category_id)
SELECT v.donation_id, bc.id
FROM (
    VALUES
        -- 2003: UGM-targeted open event (matches hero.ugm@mail.ugm.ac.id)
        ('00000000-0000-0000-0000-000000002003'::uuid, 'Universitas Gadjah Mada'),
        -- 2004: UNY & KIPK-targeted open event (tests 403 eligibility denial for UGM hero)
        ('00000000-0000-0000-0000-000000002004'::uuid, 'Universitas Negeri Yogyakarta'),
        ('00000000-0000-0000-0000-000000002004'::uuid, 'KIPK'),
        -- 2008: Completed historical targeted event
        ('00000000-0000-0000-0000-000000002008'::uuid, 'Masyarakat Umum'),
        ('00000000-0000-0000-0000-000000002008'::uuid, 'Yayasan Sayap Ibu')
) AS v(donation_id, category_name)
JOIN beneficiary_categories bc ON bc.name = v.category_name;

-- +goose Down
TRUNCATE TABLE donation_target CASCADE;
