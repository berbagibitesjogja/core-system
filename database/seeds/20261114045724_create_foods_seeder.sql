-- +goose Up

INSERT INTO foods (donation_id, food_item_id, quantity, unit)
SELECT v.donation_id, fi.id, v.quantity, v.unit::food_unit
FROM (
    VALUES
        ('00000000-0000-0000-0000-000000002001'::uuid, 'Nasi Putih', 60.00, 'portion'),
        ('00000000-0000-0000-0000-000000002001'::uuid, 'Ayam Goreng', 60.00, 'piece'),
        ('00000000-0000-0000-0000-000000002001'::uuid, 'Air Mineral', 60.00, 'pack'),

        ('00000000-0000-0000-0000-000000002002'::uuid, 'Nasi Putih', 50.00, 'portion'),
        ('00000000-0000-0000-0000-000000002002'::uuid, 'Sayur Lodeh', 15.00, 'l'),
        ('00000000-0000-0000-0000-000000002002'::uuid, 'Tempe Goreng', 100.00, 'piece'),

        ('00000000-0000-0000-0000-000000002003'::uuid, 'Bakmi Goreng', 40.00, 'portion'),
        ('00000000-0000-0000-0000-000000002003'::uuid, 'Ayam Kecap', 40.00, 'portion'),
        ('00000000-0000-0000-0000-000000002003'::uuid, 'Pisang', 40.00, 'piece'),

        ('00000000-0000-0000-0000-000000002004'::uuid, 'Nasi Kuning', 30.00, 'portion'),
        ('00000000-0000-0000-0000-000000002004'::uuid, 'Tahu Bacem', 60.00, 'piece'),

        ('00000000-0000-0000-0000-000000002005'::uuid, 'Arem-Arem', 50.00, 'piece'),
        ('00000000-0000-0000-0000-000000002005'::uuid, 'Air Mineral', 25.00, 'pack'),

        ('00000000-0000-0000-0000-000000002006'::uuid, 'Nasi Putih', 20.00, 'portion'),
        ('00000000-0000-0000-0000-000000002006'::uuid, 'Ayam Goreng', 20.00, 'piece'),

        ('00000000-0000-0000-0000-000000002007'::uuid, 'Bakmi Goreng', 40.00, 'portion'),
        ('00000000-0000-0000-0000-000000002007'::uuid, 'Air Mineral', 40.00, 'pack'),

        ('00000000-0000-0000-0000-000000002008'::uuid, 'Nasi Putih', 80.00, 'portion'),
        ('00000000-0000-0000-0000-000000002008'::uuid, 'Ayam Kecap', 80.00, 'portion'),
        ('00000000-0000-0000-0000-000000002008'::uuid, 'Pisang', 80.00, 'piece'),

        ('00000000-0000-0000-0000-000000002009'::uuid, 'Nasi Kuning', 35.00, 'portion'),
        ('00000000-0000-0000-0000-000000002009'::uuid, 'Tempe Goreng', 70.00, 'piece')
) AS v(donation_id, food_name, quantity, unit)
JOIN food_items fi ON fi.name = v.food_name;

-- +goose Down
TRUNCATE TABLE foods CASCADE;
