-- +goose Up

INSERT INTO sponsors (id, name, variant, address, email, phone, hidden, hide_donations) VALUES
    (
        '00000000-0000-0000-0000-000000001001',
        'PT Berbagi Bites Sleman',
        'company',
        'Jl. Kaliurang KM 7, Sleman, DI Yogyakarta',
        'csr@berbagibitessleman.id',
        '6281100001001',
        FALSE,
        FALSE
    ),
    (
        '00000000-0000-0000-0000-000000001002',
        'Warung Makan Bu Tini',
        'individual',
        'Jl. Gejayan No. 45, Depok, Sleman, DI Yogyakarta',
        'butini.warung@example.com',
        '6281200001002',
        FALSE,
        FALSE
    ),
    (
        '00000000-0000-0000-0000-000000001003',
        'Hotel Tentrem Yogyakarta',
        'company',
        'Jl. P. Mangkubumi No. 72A, Jetis, Kota Yogyakarta',
        'partnership@hoteltentrem.example.com',
        '622740001003',
        FALSE,
        TRUE
    ),
    (
        '00000000-0000-0000-0000-000000001004',
        'Katering Berkah Malioboro',
        'company',
        'Jl. Malioboro No. 118, Gedongtengen, Kota Yogyakarta',
        'order@berkahmalioboro.example.com',
        '6281300001004',
        FALSE,
        FALSE
    ),
    (
        '00000000-0000-0000-0000-000000001005',
        'Donatur Anonim Jogja',
        'individual',
        NULL,
        NULL,
        NULL,
        TRUE,
        TRUE
    );

-- +goose Down
TRUNCATE TABLE sponsors CASCADE;
