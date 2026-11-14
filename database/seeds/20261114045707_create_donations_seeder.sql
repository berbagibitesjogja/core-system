-- +goose Up

INSERT INTO donations (
    id,
    sponsor_id,
    created_by,
    event_at,
    quota,
    hero_quota,
    remain,
    address,
    maps,
    message,
    status,
    hide_from_public,
    hide_sponsor
) VALUES
    -- 2001: Draft donation (ready for editing, war-slot configuration, and publishing)
    (
        '00000000-0000-0000-0000-000000002001',
        '00000000-0000-0000-0000-000000001001',
        '00000000-0000-0000-0000-000000000006',
        NOW() + INTERVAL '3 days',
        60,
        40,
        40,
        'Bundaran UGM, Caturtunggal, Depok, Sleman',
        'https://maps.google.com/?q=-7.771385,110.377499',
        'Distribusi makan siang gratis (draft persiapan)',
        'draft',
        TRUE,
        FALSE
    ),
    -- 2002: Open general donation (> 24h away, public, untargeted)
    (
        '00000000-0000-0000-0000-000000002002',
        '00000000-0000-0000-0000-000000001002',
        '00000000-0000-0000-0000-000000000006',
        NOW() + INTERVAL '2 days',
        50,
        30,
        30,
        'Jl. Gejayan No. 45, Depok, Sleman',
        'https://maps.google.com/?q=-7.775120,110.389410',
        'Makan malam hangat terbuka untuk semua Hero',
        'open',
        FALSE,
        FALSE
    ),
    -- 2003: Open targeted donation (UGM only -> hero.ugm eligible, others rejected)
    (
        '00000000-0000-0000-0000-000000002003',
        '00000000-0000-0000-0000-000000001003',
        '00000000-0000-0000-0000-000000000005',
        NOW() + INTERVAL '36 hours',
        40,
        25,
        25,
        'Gelanggang Mahasiswa UGM, Bulaksumur, Yogyakarta',
        'https://maps.google.com/?q=-7.774500,110.374600',
        'Berbagi makanan khusus mahasiswa UGM',
        'open',
        FALSE,
        FALSE
    ),
    -- 2004: Open inside 12h late-cancellation window (targeted UNY + KIPK for mismatch testing)
    (
        '00000000-0000-0000-0000-000000002004',
        '00000000-0000-0000-0000-000000001004',
        '00000000-0000-0000-0000-000000000007',
        NOW() + INTERVAL '4 hours',
        30,
        20,
        20,
        'Gerbang Utama UNY, Karangmalang, Yogyakarta',
        'https://maps.google.com/?q=-7.774800,110.386300',
        'Paket makan sore khusus penerima UNY & KIPK',
        'open',
        FALSE,
        FALSE
    ),
    -- 2005: Open inside active check-in window (event_at - 1h .. event_at + 4h)
    (
        '00000000-0000-0000-0000-000000002005',
        '00000000-0000-0000-0000-000000001001',
        '00000000-0000-0000-0000-000000000006',
        NOW() + INTERVAL '30 minutes',
        25,
        15,
        15,
        'Titik Nol Kilometer Yogyakarta, Gondomanan',
        'https://maps.google.com/?q=-7.801389,110.364722',
        'Aksi berbagi cepat — window check-in aktif',
        'open',
        FALSE,
        FALSE
    ),
    -- 2006: Open donation with zero remaining hero slots (triggers 409 WAR_001)
    (
        '00000000-0000-0000-0000-000000002006',
        '00000000-0000-0000-0000-000000001002',
        '00000000-0000-0000-0000-000000000006',
        NOW() + INTERVAL '1 day',
        20,
        10,
        0,
        'Alun-Alun Kidul, Kraton, Kota Yogyakarta',
        'https://maps.google.com/?q=-7.811800,110.363200',
        'Kuota hero sudah penuh untuk pengujian konflik kuota',
        'open',
        FALSE,
        FALSE
    ),
    -- 2007: Closed past donation (suitable for attendance marking tests)
    (
        '00000000-0000-0000-0000-000000002007',
        '00000000-0000-0000-0000-000000001004',
        '00000000-0000-0000-0000-000000000006',
        NOW() - INTERVAL '3 hours',
        40,
        30,
        5,
        'Stasiun Tugu Pintu Selatan, Yogyakarta',
        'https://maps.google.com/?q=-7.789200,110.363400',
        'Distribusi malam hari telah ditutup',
        'closed',
        FALSE,
        FALSE
    ),
    -- 2008: Completed donation (history & ban burn-off baseline)
    (
        '00000000-0000-0000-0000-000000002008',
        '00000000-0000-0000-0000-000000001001',
        '00000000-0000-0000-0000-000000000005',
        NOW() - INTERVAL '2 days',
        80,
        50,
        0,
        'Pasar Beringharjo, Malioboro, Kota Yogyakarta',
        'https://maps.google.com/?q=-7.798700,110.365900',
        'Distribusi selesai sukses dan terarsip',
        'completed',
        FALSE,
        FALSE
    ),
    -- 2009: Cancelled donation with hidden sponsor
    (
        '00000000-0000-0000-0000-000000002009',
        '00000000-0000-0000-0000-000000001005',
        '00000000-0000-0000-0000-000000000006',
        NOW() + INTERVAL '5 days',
        35,
        20,
        20,
        'Kotabaru, Gondokusuman, Kota Yogyakarta',
        NULL,
        'Dibatalkan oleh sponsor karena perubahan jadwal',
        'cancelled',
        TRUE,
        TRUE
    );

-- +goose Down
TRUNCATE TABLE donations CASCADE;
