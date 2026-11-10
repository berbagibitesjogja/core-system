-- +goose Up
INSERT INTO beneficiary_categories (name, variant, email_domain, is_priority, hidden) VALUES
    ('Universitas Gadjah Mada', 'university', 'ugm.ac.id', FALSE, FALSE),
    ('Universitas Negeri Yogyakarta', 'university', 'uny.ac.id', FALSE, FALSE),
    ('Universitas Islam Indonesia', 'university', 'uii.ac.id', FALSE, FALSE),
    ('ISI Yogyakarta', 'university', 'isi.ac.id', FALSE, FALSE),
    ('KIPK', 'kipk', NULL, TRUE, FALSE),
    ('Masyarakat Umum', 'society', NULL, FALSE, FALSE),
    ('Yayasan Hamba', 'foundation', NULL, FALSE, FALSE),
    ('BRSPA', 'shelter', NULL, FALSE, FALSE),
    ('Panti Sosial Tresna Werdha', 'nursing_home', NULL, FALSE, FALSE),
    ('Yayasan Madania', 'foundation', NULL, FALSE, FALSE),
    ('Yayasan Sayap Ibu', 'orphanage', NULL, FALSE, FALSE),
    ('Rumah Singgah Pasien Anak', 'shelter', NULL, FALSE, FALSE),
    ('TPA Samirono', 'religious_community', NULL, FALSE, FALSE),
    ('Rumah Yoel', 'shelter', NULL, FALSE, FALSE),
    ('Yayasan Al-Kahfi', 'foundation', NULL, FALSE, FALSE),
    ('Yayasan La Tahzan', 'foundation', NULL, FALSE, FALSE),
    ('Pesantren Al-Muyamman', 'religious_community', NULL, FALSE, FALSE),
    ('Yayasan Al-afiyah', 'foundation', NULL, FALSE, FALSE),
    ('Rumah Yatim Piatu Aisyiyah', 'orphanage', NULL, FALSE, FALSE),
    ('Pondok Pesantren Jamhariyah', 'religious_community', NULL, FALSE, FALSE),
    ('Panti Asuhan Ibadah Bunda', 'orphanage', NULL, FALSE, FALSE),
    ('Forkom Yogyakarta', 'society', NULL, FALSE, FALSE),
    ('RPA Wiloso Projo', 'orphanage', NULL, FALSE, FALSE),
    ('Yayasan Atap Langit', 'foundation', NULL, FALSE, FALSE),
    ('Panti Asuhan Yatim Putra Muhammadiyah', 'orphanage', NULL, FALSE, FALSE),
    ('Yayasan Bumi Damai', 'foundation', NULL, FALSE, FALSE);

-- +goose Down
TRUNCATE TABLE beneficiary_categories CASCADE;
