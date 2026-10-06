CREATE TABLE reference_region (
    code text PRIMARY KEY CHECK (code ~ '^[0-9]{2}$' AND code <> '99'),
    name text NOT NULL UNIQUE,
    seed_version text NOT NULL,
    source_url text NOT NULL
);

INSERT INTO reference_region (code, name, seed_version, source_url) VALUES
('01', 'Республика Адыгея', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('02', 'Республика Башкортостан', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('03', 'Республика Бурятия', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('04', 'Республика Алтай', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('05', 'Республика Дагестан', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('06', 'Республика Ингушетия', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('07', 'Кабардино-Балкарская Республика', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('08', 'Республика Калмыкия', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('09', 'Карачаево-Черкесская Республика', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('10', 'Республика Карелия', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('11', 'Республика Коми', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('12', 'Республика Марий Эл', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('13', 'Республика Мордовия', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('14', 'Республика Саха (Якутия)', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('15', 'Республика Северная Осетия - Алания', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('16', 'Республика Татарстан', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('17', 'Республика Тыва', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('18', 'Удмуртская Республика', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('19', 'Республика Хакасия', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('20', 'Чеченская Республика', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('21', 'Чувашская Республика', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('22', 'Алтайский край', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('23', 'Краснодарский край', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('24', 'Красноярский край', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('25', 'Приморский край', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('26', 'Ставропольский край', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('27', 'Хабаровский край', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('28', 'Амурская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('29', 'Архангельская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('30', 'Астраханская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('31', 'Белгородская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('32', 'Брянская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('33', 'Владимирская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('34', 'Волгоградская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('35', 'Вологодская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('36', 'Воронежская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('37', 'Ивановская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('38', 'Иркутская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('39', 'Калининградская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('40', 'Калужская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('41', 'Камчатский край', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('42', 'Кемеровская область - Кузбасс', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('43', 'Кировская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('44', 'Костромская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('45', 'Курганская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('46', 'Курская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('47', 'Ленинградская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('48', 'Липецкая область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('49', 'Магаданская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('50', 'Московская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('51', 'Мурманская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('52', 'Нижегородская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('53', 'Новгородская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('54', 'Новосибирская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('55', 'Омская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('56', 'Оренбургская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('57', 'Орловская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('58', 'Пензенская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('59', 'Пермский край', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('60', 'Псковская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('61', 'Ростовская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('62', 'Рязанская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('63', 'Самарская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('64', 'Саратовская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('65', 'Сахалинская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('66', 'Свердловская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('67', 'Смоленская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('68', 'Тамбовская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('69', 'Тверская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('70', 'Томская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('71', 'Тульская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('72', 'Тюменская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('73', 'Ульяновская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('74', 'Челябинская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('75', 'Забайкальский край', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('76', 'Ярославская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('77', 'Москва', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('78', 'Санкт-Петербург', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('79', 'Еврейская автономная область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('80', 'Донецкая Народная Республика', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('81', 'Луганская Народная Республика', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('82', 'Республика Крым', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('83', 'Ненецкий автономный округ', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('84', 'Херсонская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('85', 'Запорожская область', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('86', 'Ханты-Мансийский автономный округ - Югра', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('87', 'Чукотский автономный округ', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('89', 'Ямало-Ненецкий автономный округ', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/'),
('92', 'Севастополь', 'fns-ssrf-2025-10-01', 'https://www.nalog.gov.ru/rn77/program/5961292/');

CREATE TABLE residential_complex_catalog_snapshot (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    region_code text NOT NULL REFERENCES reference_region(code),
    observed_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL CHECK (expires_at = observed_at + interval '24 hours'),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX residential_complex_catalog_latest_idx
    ON residential_complex_catalog_snapshot (region_code, observed_at DESC);

CREATE TABLE residential_complex_catalog_item (
    snapshot_id uuid NOT NULL REFERENCES residential_complex_catalog_snapshot(id),
    complex_id text NOT NULL,
    full_name text NOT NULL,
    PRIMARY KEY (snapshot_id, complex_id)
);

CREATE TABLE residential_complex_catalog_page_checkpoint (
    region_code text NOT NULL REFERENCES reference_region(code),
    page integer NOT NULL CHECK (page >= 0),
    page_size integer NOT NULL CHECK (page_size > 0),
    total bigint NOT NULL CHECK (total >= 0),
    items jsonb NOT NULL CHECK (jsonb_typeof(items) = 'array'),
    observed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (region_code, page)
);

CREATE TABLE source_request_gate (
    source_key text PRIMARY KEY,
    next_allowed_at timestamptz NOT NULL
);

INSERT INTO source_request_gate (source_key, next_allowed_at)
VALUES ('eiszhs', TIMESTAMPTZ '1970-01-01 00:00:00+00');

---- create above / drop below ----

DROP TABLE source_request_gate;
DROP TABLE residential_complex_catalog_page_checkpoint;
DROP TABLE residential_complex_catalog_item;
DROP TABLE residential_complex_catalog_snapshot;
DROP TABLE reference_region;
