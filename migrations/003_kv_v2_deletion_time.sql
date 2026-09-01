ALTER TABLE secret_versions
    ADD COLUMN IF NOT EXISTS deletion_time timestamptz;

CREATE INDEX IF NOT EXISTS secret_versions_deleted_idx
    ON secret_versions(secret_id, version)
    WHERE deletion_time IS NOT NULL;
