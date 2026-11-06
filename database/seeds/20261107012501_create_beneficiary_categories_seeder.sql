-- +goose Up
INSERT INTO beneficiary_categories (name, variant, hidden) VALUES
    ('Universitas Gadjah Mada', 'student', FALSE),
    ('Masyarakat Umum', 'society', FALSE),
    ('Yayasan Hamba', 'foundation', FALSE),
    ('BRSPA', 'shelter', FALSE),
    ('Panti Sosial Tresna Werdha', 'nursing_home', FALSE),
    ('Yayasan Madania', 'foundation', FALSE),
    ('Yayasan Sayap Ibu', 'orphanage', FALSE),
    ('Rumah Singgah Pasien Anak', 'shelter', FALSE),
    ('TPA Samirono', 'religious_community', FALSE),
    ('Rumah Yoel', 'shelter', FALSE),
    ('Yayasan Al-Kahfi', 'foundation', FALSE),
    ('Yayasan La Tahzan', 'foundation', FALSE),
    ('Pesantren Al-Muyamman', 'religious_community', FALSE),
    ('Yayasan Al-afiyah', 'foundation', FALSE),
    ('Rumah Yatim Piatu Aisyiyah', 'orphanage', FALSE),
    ('Pondok Pesantren Jamhariyah', 'religious_community', FALSE),
    ('ISI Yogyakarta', 'student', FALSE),
    ('Panti Asuhan Ibadah Bunda', 'orphanage', FALSE),
    ('Forkom Yogyakarta', 'society', FALSE),
    ('RPA Wiloso Projo', 'orphanage', FALSE),
    ('Yayasan Atap Langit', 'foundation', FALSE),
    ('Panti Asuhan Yatim Putra Muhammadiyah', 'orphanage', FALSE),
    ('Yayasan Bumi Damai', 'foundation', FALSE);

-- +goose Down
DELETE FROM beneficiary_categories;
