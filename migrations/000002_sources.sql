-- +goose Up

-- Renames preserve existing WHO rows and immutable snapshots.
ALTER TABLE dataset RENAME COLUMN who_indicator_id TO indicator_id;
ALTER TABLE dataset RENAME COLUMN who_measure_code TO indicator_code;
ALTER TABLE dataset ADD COLUMN source_system TEXT NOT NULL DEFAULT 'WHO';
ALTER TABLE dataset DROP CONSTRAINT dataset_who_indicator_id_key;
ALTER TABLE dataset ADD UNIQUE (source_system, indicator_id);
ALTER TABLE dataset_release RENAME COLUMN raw_csv TO raw_artifact;
ALTER TABLE dataset_release RENAME COLUMN csv_headers TO source_fields;
ALTER TABLE import_job RENAME COLUMN staged_raw_csv TO staged_raw_artifact;
ALTER TABLE observation ADD COLUMN source_note TEXT NOT NULL DEFAULT '';
ALTER TABLE observation ADD COLUMN source_status TEXT NOT NULL DEFAULT '';
