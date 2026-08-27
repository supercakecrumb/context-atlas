-- name: InsertDataset :one
INSERT INTO dataset (id, who_indicator_id, who_measure_code, title, source_url, definition)
VALUES ($1, $2, $3, $4, $5, sqlc.arg(definition)::jsonb)
RETURNING *;

-- name: InsertMeasure :one
INSERT INTO measure (dataset_id, code, title, description)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: InsertSeries :one
INSERT INTO series (dataset_id, measure_code, label, dimensions, dimensions_hash, unit, statistic, value_kind)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: InsertSourceGeography :one
WITH inserted AS (
    INSERT INTO source_geography (source_system, source_code, name, geography_kind, canonical_m49_code)
    VALUES ($1, $2, $3, $4, $5)
    ON CONFLICT (source_system, source_code, geography_kind, name, canonical_m49_code) DO NOTHING
    RETURNING *
)
SELECT * FROM inserted
UNION ALL
SELECT *
FROM source_geography
WHERE source_system = $1
  AND source_code = $2
  AND name = $3
  AND geography_kind = $4
  AND canonical_m49_code IS NOT DISTINCT FROM $5
LIMIT 1;

-- name: InsertDatasetRelease :one
INSERT INTO dataset_release (
    dataset_id, source_url, citation, accessed_at, raw_csv, sha256, response_metadata, csv_headers,
    schema_fingerprint, parser_version, source_row_count, imported_row_count,
    duplicate_row_count, rejected_row_count, diagnostics
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15
)
RETURNING *;

-- name: InsertObservation :one
INSERT INTO observation (
    dataset_id, release_id, series_id, source_geography_id, year, raw_value,
    display_value, numeric_value, lower_bound, upper_bound, value_status,
    publish_state, source_row_key, canonical_m49_code
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
RETURNING *;

-- name: InsertM49ReferenceRelease :one
INSERT INTO m49_reference_release (classification_version, source_url, accessed_at, raw_payload, sha256)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: InsertM49Geography :one
INSERT INTO m49_geography (m49_release_id, code, name, geography_kind, is_leaf, iso_alpha2, iso_alpha3)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: InsertM49Group :one
INSERT INTO m49_group (m49_release_id, code, parent_code, name, group_kind, is_custom)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: InsertM49GroupMember :exec
INSERT INTO m49_group_member (m49_release_id, group_code, geography_code)
VALUES ($1, $2, $3);

-- name: InsertImportJob :one
INSERT INTO import_job (dataset_id, requested_by, import_kind, state, source_url, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: UpdateImportJob :one
UPDATE import_job
SET release_id = $2,
    state = $3,
    discovered_url = $4,
    staged_raw_csv = $5,
    preview = $6,
    error_message = $7,
    started_at = $8,
    finished_at = $9
WHERE id = $1
RETURNING *;

-- name: GetM49ReferenceRelease :one
SELECT * FROM m49_reference_release WHERE id = $1;

-- name: ListReleaseDatasetPairsByIDs :many
SELECT id, dataset_id
FROM dataset_release
WHERE id = ANY($1::bigint[])
ORDER BY id;

-- name: ListDatasetsWithRelease :many
SELECT DISTINCT dataset_id
FROM dataset_release
ORDER BY dataset_id;

-- name: InsertCatalogSnapshot :one
INSERT INTO catalog_snapshot (id, m49_release_id)
VALUES ($1, $2)
RETURNING *;

-- name: InsertCatalogSnapshotRelease :exec
INSERT INTO catalog_snapshot_release (snapshot_id, dataset_id, release_id)
VALUES ($1, $2, $3);

-- name: SetCatalogHead :exec
INSERT INTO catalog_head (singleton, snapshot_id)
VALUES (true, $1)
ON CONFLICT (singleton) DO UPDATE
SET snapshot_id = EXCLUDED.snapshot_id, updated_at = now();

-- name: GetCatalogHead :one
SELECT s.*
FROM catalog_head h
JOIN catalog_snapshot s ON s.id = h.snapshot_id
WHERE h.singleton;

-- name: ListCatalogSnapshotReleases :many
SELECT csr.snapshot_id, csr.dataset_id, csr.release_id, r.accessed_at, r.source_url, r.citation
FROM catalog_snapshot_release csr
JOIN dataset_release r ON r.id = csr.release_id
WHERE csr.snapshot_id = $1
ORDER BY csr.dataset_id;

-- name: ListCatalogDatasets :many
SELECT d.id AS dataset_id, d.who_indicator_id, d.who_measure_code, d.title,
       r.id AS release_id, r.accessed_at, r.source_url, r.citation
FROM catalog_snapshot_release csr
JOIN dataset d ON d.id = csr.dataset_id
JOIN dataset_release r ON r.id = csr.release_id
WHERE csr.snapshot_id = $1
ORDER BY d.title;

-- name: ListGroupsForM49Release :many
SELECT g.m49_release_id, g.code, g.parent_code, g.name, g.group_kind, g.is_custom,
       m.geography_code
FROM m49_group g
LEFT JOIN m49_group_member m
    ON m.m49_release_id = g.m49_release_id AND m.group_code = g.code
WHERE g.m49_release_id = $1
ORDER BY g.group_kind, g.name, m.geography_code;

-- name: SearchSourceGeographies :many
SELECT *
FROM source_geography
WHERE $1 = '' OR name ILIKE '%' || $1 || '%' OR source_code ILIKE '%' || $1 || '%'
ORDER BY name, source_code
LIMIT $2;

-- name: ListSeriesYearsForSnapshot :many
SELECT DISTINCT o.year
FROM catalog_snapshot_release csr
JOIN observation o ON o.release_id = csr.release_id
WHERE csr.snapshot_id = $1 AND o.series_id = $2
ORDER BY o.year;

-- name: ListObservationsForSeries :many
SELECT o.*
FROM catalog_snapshot_release csr
JOIN observation o ON o.release_id = csr.release_id
WHERE csr.snapshot_id = $1
  AND o.series_id = $2
  AND o.year = ANY($3::smallint[])
ORDER BY o.year, o.source_geography_id
LIMIT $4 OFFSET $5;

-- name: CountObservationsForSeries :one
SELECT count(*)
FROM catalog_snapshot_release csr
JOIN observation o ON o.release_id = csr.release_id
WHERE csr.snapshot_id = $1
  AND o.series_id = $2
  AND o.year = ANY($3::smallint[]);

-- name: ListAssociationPoints :many
SELECT x.canonical_m49_code, x.numeric_value AS x_value, y.numeric_value AS y_value
FROM catalog_snapshot_release sx
JOIN observation x ON x.release_id = sx.release_id
JOIN catalog_snapshot_release sy ON sy.snapshot_id = sx.snapshot_id
JOIN observation y ON y.release_id = sy.release_id
WHERE sx.snapshot_id = $1
  AND x.series_id = $2
  AND x.year = $3
  AND y.series_id = $4
  AND y.year = $5
  AND x.value_status = 'numeric'
  AND y.value_status = 'numeric'
  AND x.publish_state = 'PUBLISHED'
  AND y.publish_state = 'PUBLISHED'
  AND x.canonical_m49_code IS NOT NULL
  AND x.canonical_m49_code = y.canonical_m49_code
  AND (coalesce(cardinality($6::text[]), 0) = 0 OR x.canonical_m49_code = ANY($6::text[]))
ORDER BY x.canonical_m49_code;

-- name: CalculateAssociation :one
SELECT count(*)::bigint AS paired_n,
       coalesce(
           corr(x.numeric_value::double precision, y.numeric_value::double precision),
           'NaN'::double precision
       )::text AS pearson_r
FROM catalog_snapshot_release sx
JOIN observation x ON x.release_id = sx.release_id
JOIN catalog_snapshot_release sy ON sy.snapshot_id = sx.snapshot_id
JOIN observation y ON y.release_id = sy.release_id
WHERE sx.snapshot_id = $1
  AND x.series_id = $2
  AND x.year = $3
  AND y.series_id = $4
  AND y.year = $5
  AND x.value_status = 'numeric'
  AND y.value_status = 'numeric'
  AND x.publish_state = 'PUBLISHED'
  AND y.publish_state = 'PUBLISHED'
  AND x.canonical_m49_code IS NOT NULL
  AND x.canonical_m49_code = y.canonical_m49_code
  AND (coalesce(cardinality($6::text[]), 0) = 0 OR x.canonical_m49_code = ANY($6::text[]));

-- name: InsertAuthIntent :one
INSERT INTO auth_intents (
    id, code, messenger, audience, subject_id, state, identity_json, metadata_json,
    redemption_mode, max_redemptions, redemption_count, expires_at, created_at, consumed_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
RETURNING *;

-- name: GetAuthIntentByMessengerCode :one
SELECT *
FROM auth_intents
WHERE messenger = $1 AND code = $2;

-- name: ConsumeAuthIntent :one
UPDATE auth_intents
SET redemption_count = $2, consumed_at = $3, state = $4
WHERE id = $1 AND redemption_count = $5 AND state = 'active'
RETURNING *;

-- name: DeleteExpiredAuthIntents :execrows
DELETE FROM auth_intents
WHERE expires_at IS NOT NULL AND expires_at < $1;

-- name: InsertWebSession :one
INSERT INTO web_sessions (session_id, subject_id, token_hash, issued_at, expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetWebSessionByTokenHash :one
SELECT * FROM web_sessions WHERE token_hash = $1;

-- name: DeleteWebSessionByTokenHash :execrows
DELETE FROM web_sessions WHERE token_hash = $1;
