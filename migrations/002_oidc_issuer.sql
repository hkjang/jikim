ALTER TABLE users ADD COLUMN IF NOT EXISTS external_issuer text;

DROP INDEX IF EXISTS users_external_subject_idx;

CREATE UNIQUE INDEX IF NOT EXISTS users_external_identity_idx
    ON users (auth_source, external_issuer, external_subject)
    WHERE external_subject IS NOT NULL AND external_issuer IS NOT NULL;
