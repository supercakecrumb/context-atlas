-- +goose Up

CREATE TABLE dataset (
    id               TEXT PRIMARY KEY CHECK (id ~ '^[a-z0-9][a-z0-9_-]{0,127}$'),
    who_indicator_id TEXT NOT NULL UNIQUE,
    who_measure_code TEXT NOT NULL,
    title            TEXT NOT NULL,
    source_url       TEXT NOT NULL CHECK (source_url ~ '^https://'),
    definition       JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(definition) = 'object'),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE measure (
    dataset_id  TEXT NOT NULL REFERENCES dataset(id),
    code        TEXT NOT NULL,
    title       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (dataset_id, code)
);

CREATE TABLE dataset_release (
    id                  BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    dataset_id          TEXT NOT NULL REFERENCES dataset(id),
    source_url          TEXT NOT NULL CHECK (source_url ~ '^https://'),
    citation            TEXT NOT NULL,
    accessed_at         TIMESTAMPTZ NOT NULL,
    raw_csv             BYTEA NOT NULL CHECK (octet_length(raw_csv) <= 52428800),
    sha256              TEXT NOT NULL CHECK (sha256 ~ '^[a-f0-9]{64}$'),
    response_metadata   JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(response_metadata) = 'object'),
    csv_headers         JSONB NOT NULL CHECK (jsonb_typeof(csv_headers) = 'array'),
    schema_fingerprint  TEXT NOT NULL CHECK (schema_fingerprint ~ '^[a-f0-9]{64}$'),
    parser_version      TEXT NOT NULL,
    source_row_count    INTEGER NOT NULL CHECK (source_row_count >= 0),
    imported_row_count  INTEGER NOT NULL CHECK (imported_row_count >= 0),
    duplicate_row_count INTEGER NOT NULL CHECK (duplicate_row_count >= 0),
    rejected_row_count  INTEGER NOT NULL CHECK (rejected_row_count >= 0),
    diagnostics         JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(diagnostics) = 'object'),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (dataset_id, sha256),
    UNIQUE (id, dataset_id),
    CHECK (source_row_count = imported_row_count + duplicate_row_count + rejected_row_count)
);

CREATE TABLE series (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    dataset_id      TEXT NOT NULL,
    measure_code    TEXT NOT NULL,
    label           TEXT NOT NULL,
    dimensions      JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(dimensions) = 'object'),
    dimensions_hash TEXT NOT NULL CHECK (dimensions_hash ~ '^[a-f0-9]{64}$'),
    unit            TEXT NOT NULL,
    statistic       TEXT NOT NULL,
    value_kind      TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (dataset_id, measure_code, dimensions_hash, unit, statistic, value_kind),
    UNIQUE (id, dataset_id),
    FOREIGN KEY (dataset_id, measure_code) REFERENCES measure(dataset_id, code)
);

CREATE TABLE source_geography (
    id                 BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    source_system      TEXT NOT NULL DEFAULT 'WHO',
    source_code        TEXT NOT NULL,
    name               TEXT NOT NULL,
    geography_kind     TEXT NOT NULL,
    canonical_m49_code TEXT CHECK (canonical_m49_code IS NULL OR canonical_m49_code ~ '^[0-9]{3}$'),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE NULLS NOT DISTINCT (source_system, source_code, geography_kind, name, canonical_m49_code)
);

CREATE TABLE observation (
    id                    BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    dataset_id            TEXT NOT NULL,
    release_id            BIGINT NOT NULL,
    series_id             BIGINT NOT NULL,
    source_geography_id   BIGINT NOT NULL REFERENCES source_geography(id),
    year                  SMALLINT NOT NULL CHECK (year BETWEEN 0 AND 9999),
    raw_value             TEXT NOT NULL,
    display_value         TEXT NOT NULL,
    numeric_value         NUMERIC,
    lower_bound           NUMERIC,
    upper_bound           NUMERIC,
    value_status          TEXT NOT NULL CHECK (value_status IN ('numeric', 'missing', 'suppressed', 'not_applicable')),
    publish_state         TEXT NOT NULL,
    source_row_key        TEXT NOT NULL,
    canonical_m49_code    TEXT CHECK (canonical_m49_code IS NULL OR canonical_m49_code ~ '^[0-9]{3}$'),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (release_id, series_id, source_geography_id, year),
    FOREIGN KEY (release_id, dataset_id) REFERENCES dataset_release(id, dataset_id),
    FOREIGN KEY (series_id, dataset_id) REFERENCES series(id, dataset_id),
    CHECK (
        (value_status = 'numeric' AND numeric_value IS NOT NULL) OR
        (value_status <> 'numeric' AND numeric_value IS NULL AND lower_bound IS NULL AND upper_bound IS NULL)
    ),
    CHECK (lower_bound IS NULL OR upper_bound IS NULL OR lower_bound <= upper_bound),
    CHECK (lower_bound IS NULL OR numeric_value IS NULL OR lower_bound <= numeric_value),
    CHECK (upper_bound IS NULL OR numeric_value IS NULL OR numeric_value <= upper_bound)
);

CREATE TABLE m49_reference_release (
    id                     BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    classification_version TEXT NOT NULL,
    source_url             TEXT NOT NULL CHECK (source_url ~ '^https://'),
    accessed_at            TIMESTAMPTZ NOT NULL,
    raw_payload            BYTEA NOT NULL CHECK (octet_length(raw_payload) <= 52428800),
    sha256                 TEXT NOT NULL CHECK (sha256 ~ '^[a-f0-9]{64}$'),
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (sha256)
);

CREATE TABLE m49_geography (
    m49_release_id BIGINT NOT NULL REFERENCES m49_reference_release(id),
    code           TEXT NOT NULL CHECK (code ~ '^[0-9]{3}$'),
    name           TEXT NOT NULL,
    geography_kind TEXT NOT NULL,
    is_leaf        BOOLEAN NOT NULL,
    iso_alpha2     TEXT,
    iso_alpha3     TEXT,
    PRIMARY KEY (m49_release_id, code)
);

CREATE TABLE m49_group (
    m49_release_id BIGINT NOT NULL REFERENCES m49_reference_release(id),
    code           TEXT NOT NULL,
    parent_code    TEXT,
    name           TEXT NOT NULL,
    group_kind     TEXT NOT NULL,
    is_custom      BOOLEAN NOT NULL DEFAULT false,
    PRIMARY KEY (m49_release_id, code),
    FOREIGN KEY (m49_release_id, parent_code) REFERENCES m49_group(m49_release_id, code),
    CHECK (parent_code IS NULL OR parent_code <> code)
);

CREATE TABLE m49_group_member (
    m49_release_id BIGINT NOT NULL,
    group_code     TEXT NOT NULL,
    geography_code TEXT NOT NULL CHECK (geography_code ~ '^[0-9]{3}$'),
    PRIMARY KEY (m49_release_id, group_code, geography_code),
    FOREIGN KEY (m49_release_id, group_code) REFERENCES m49_group(m49_release_id, code),
    FOREIGN KEY (m49_release_id, geography_code) REFERENCES m49_geography(m49_release_id, code)
);

CREATE TABLE catalog_snapshot (
    id                 TEXT PRIMARY KEY CHECK (id ~ '^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$'),
    m49_release_id     BIGINT NOT NULL REFERENCES m49_reference_release(id),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE catalog_snapshot_release (
    snapshot_id TEXT NOT NULL REFERENCES catalog_snapshot(id),
    dataset_id  TEXT NOT NULL,
    release_id  BIGINT NOT NULL,
    PRIMARY KEY (snapshot_id, dataset_id),
    UNIQUE (snapshot_id, release_id),
    FOREIGN KEY (release_id, dataset_id) REFERENCES dataset_release(id, dataset_id)
);

CREATE TABLE catalog_head (
    singleton   BOOLEAN PRIMARY KEY DEFAULT true CHECK (singleton),
    snapshot_id TEXT NOT NULL REFERENCES catalog_snapshot(id),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE import_job (
    id                 BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    dataset_id         TEXT REFERENCES dataset(id),
    release_id         BIGINT,
    requested_by       TEXT NOT NULL DEFAULT '',
    import_kind        TEXT NOT NULL CHECK (import_kind IN ('preview', 'manual', 'refresh', 'scheduled', 'startup', 'manual_refresh')),
    state              TEXT NOT NULL CHECK (state IN ('queued', 'running', 'preview_ready', 'confirmed', 'failed', 'expired', 'succeeded', 'unchanged', 'interrupted')),
    source_url         TEXT NOT NULL CHECK (source_url ~ '^https://'),
    discovered_url     TEXT,
    staged_raw_csv     BYTEA CHECK (staged_raw_csv IS NULL OR octet_length(staged_raw_csv) <= 52428800),
    preview            JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(preview) = 'object'),
    error_message      TEXT,
    expires_at         TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at         TIMESTAMPTZ,
    finished_at        TIMESTAMPTZ,
    FOREIGN KEY (release_id, dataset_id) REFERENCES dataset_release(id, dataset_id),
    CHECK (finished_at IS NULL OR started_at IS NOT NULL)
);

CREATE TABLE auth_intents (
    id               TEXT PRIMARY KEY,
    code             TEXT NOT NULL,
    messenger        TEXT NOT NULL,
    audience         TEXT NOT NULL DEFAULT '',
    subject_id       TEXT NOT NULL DEFAULT '',
    state            TEXT NOT NULL,
    identity_json    JSONB,
    metadata_json    JSONB,
    redemption_mode  TEXT NOT NULL CHECK (redemption_mode IN ('one_time', 'reusable')),
    max_redemptions  INTEGER NOT NULL DEFAULT 0 CHECK (max_redemptions >= 0),
    redemption_count INTEGER NOT NULL DEFAULT 0 CHECK (redemption_count >= 0),
    expires_at       TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL,
    consumed_at      TIMESTAMPTZ,
    UNIQUE (messenger, code),
    CHECK (max_redemptions = 0 OR redemption_count <= max_redemptions)
);

CREATE TABLE web_sessions (
    session_id TEXT PRIMARY KEY,
    subject_id TEXT NOT NULL,
    token_hash BYTEA NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    issued_at  TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    CHECK (expires_at > issued_at)
);

CREATE INDEX observation_series_year_idx ON observation (series_id, year, release_id);
CREATE INDEX observation_numeric_m49_idx ON observation (release_id, series_id, year, canonical_m49_code)
    WHERE value_status = 'numeric' AND canonical_m49_code IS NOT NULL;
CREATE INDEX source_geography_name_idx ON source_geography (lower(name));
CREATE INDEX m49_group_member_geography_idx ON m49_group_member (m49_release_id, geography_code);
CREATE INDEX import_job_state_created_idx ON import_job (state, created_at DESC);
CREATE INDEX auth_intents_expires_at_idx ON auth_intents (expires_at) WHERE expires_at IS NOT NULL;
CREATE INDEX web_sessions_expires_at_idx ON web_sessions (expires_at);

-- +goose StatementBegin
CREATE FUNCTION reject_immutable_row() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION '% rows are immutable', TG_TABLE_NAME USING ERRCODE = '55000';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER dataset_immutable BEFORE UPDATE OR DELETE ON dataset
    FOR EACH ROW EXECUTE FUNCTION reject_immutable_row();
CREATE TRIGGER measure_immutable BEFORE UPDATE OR DELETE ON measure
    FOR EACH ROW EXECUTE FUNCTION reject_immutable_row();
CREATE TRIGGER dataset_release_immutable BEFORE UPDATE OR DELETE ON dataset_release
    FOR EACH ROW EXECUTE FUNCTION reject_immutable_row();
CREATE TRIGGER series_immutable BEFORE UPDATE OR DELETE ON series
    FOR EACH ROW EXECUTE FUNCTION reject_immutable_row();
CREATE TRIGGER source_geography_immutable BEFORE UPDATE OR DELETE ON source_geography
    FOR EACH ROW EXECUTE FUNCTION reject_immutable_row();
CREATE TRIGGER observation_immutable BEFORE UPDATE OR DELETE ON observation
    FOR EACH ROW EXECUTE FUNCTION reject_immutable_row();
CREATE TRIGGER m49_reference_release_immutable BEFORE UPDATE OR DELETE ON m49_reference_release
    FOR EACH ROW EXECUTE FUNCTION reject_immutable_row();
CREATE TRIGGER m49_geography_immutable BEFORE UPDATE OR DELETE ON m49_geography
    FOR EACH ROW EXECUTE FUNCTION reject_immutable_row();
CREATE TRIGGER m49_group_immutable BEFORE UPDATE OR DELETE ON m49_group
    FOR EACH ROW EXECUTE FUNCTION reject_immutable_row();
CREATE TRIGGER m49_group_member_immutable BEFORE UPDATE OR DELETE ON m49_group_member
    FOR EACH ROW EXECUTE FUNCTION reject_immutable_row();
CREATE TRIGGER catalog_snapshot_immutable BEFORE UPDATE OR DELETE ON catalog_snapshot
    FOR EACH ROW EXECUTE FUNCTION reject_immutable_row();
CREATE TRIGGER catalog_snapshot_release_immutable BEFORE UPDATE OR DELETE ON catalog_snapshot_release
    FOR EACH ROW EXECUTE FUNCTION reject_immutable_row();
