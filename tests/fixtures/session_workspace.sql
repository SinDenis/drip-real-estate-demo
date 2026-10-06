INSERT INTO analytical_session (
    id, status, created_by, created_at, region_names,
    residential_complex_names, segments, period_from, period_to
)
SELECT
    '11111111-1111-4111-8111-111111111111', 'collecting', id, now(),
    ARRAY['Москва'], ARRAY['ЖК Северный'], ARRAY['Первичная недвижимость'],
    DATE '2026-01-01', DATE '2026-06-30'
FROM user_account WHERE normalized_login = 'admin';

INSERT INTO analytical_session (
    id, status, created_by, created_at, region_names,
    residential_complex_names, segments, period_from, period_to
)
SELECT
    '22222222-2222-4222-8222-222222222222', 'complete', id, now() - interval '1 hour',
    ARRAY['Москва', 'Московская область'], ARRAY['ЖК Речной', 'ЖК Лесной'],
    ARRAY['Первичная недвижимость', 'Апартаменты'], DATE '2025-07-01', DATE '2025-12-31'
FROM user_account WHERE normalized_login = 'admin';

INSERT INTO analytical_session (
    id, status, created_by, created_at, region_names,
    residential_complex_names, segments, period_from, period_to, deleted_at
)
SELECT
    '33333333-3333-4333-8333-333333333333', 'complete', id, now() - interval '2 hours',
    ARRAY['Скрытый регион'], ARRAY['Скрытый ЖК'], ARRAY['Первичная недвижимость'],
    DATE '2025-01-01', DATE '2025-06-30', now()
FROM user_account WHERE normalized_login = 'admin';

INSERT INTO analytical_session (
    status, created_by, created_at, region_names,
    residential_complex_names, segments, period_from, period_to
)
SELECT
    'complete', account.id, now() - series.number * interval '1 day',
    ARRAY['Калужская область'], ARRAY['ЖК ' || series.number::text],
    ARRAY['Новостройки'], DATE '2024-01-01', DATE '2024-12-31'
FROM user_account AS account
CROSS JOIN generate_series(1, 31) AS series(number)
WHERE account.normalized_login = 'admin';
