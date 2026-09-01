CREATE TABLE IF NOT EXISTS users (
    id text PRIMARY KEY,
    username text NOT NULL,
    display_name text NOT NULL DEFAULT '',
    email text NOT NULL DEFAULT '',
    password_hash text,
    role text NOT NULL CHECK (role IN ('admin', 'manager', 'user', 'auditor')),
    active boolean NOT NULL DEFAULT true,
    auth_source text NOT NULL DEFAULT 'local',
    external_subject text,
    personal_key_version integer NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS system_crypto_sentinel (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    ciphertext bytea NOT NULL,
    nonce bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS users_username_lower_idx ON users (lower(username));
CREATE UNIQUE INDEX IF NOT EXISTS users_external_subject_idx
    ON users (auth_source, external_subject) WHERE external_subject IS NOT NULL;

CREATE TABLE IF NOT EXISTS user_keys (
    id text PRIMARY KEY,
    user_id text NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    version integer NOT NULL,
    encrypted_key bytea NOT NULL,
    nonce bytea NOT NULL,
    permissions jsonb NOT NULL DEFAULT '{"encrypt":true,"decrypt":true,"rotate":true}'::jsonb,
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(user_id, version)
);

CREATE TABLE IF NOT EXISTS sessions (
    token_hash bytea PRIMARY KEY,
    id text NOT NULL UNIQUE,
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind text NOT NULL DEFAULT 'session' CHECK (kind IN ('session', 'openbao', 'api')),
    name text NOT NULL DEFAULT '',
    created_by text REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz
);

CREATE INDEX IF NOT EXISTS sessions_user_idx ON sessions(user_id);
CREATE INDEX IF NOT EXISTS sessions_expiry_idx ON sessions(expires_at) WHERE revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS oidc_login_codes (
    code_hash bytea PRIMARY KEY,
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS oidc_login_codes_expiry_idx ON oidc_login_codes(expires_at);

CREATE TABLE IF NOT EXISTS applications (
    id text PRIMARY KEY,
    name text NOT NULL UNIQUE,
    description text NOT NULL DEFAULT '',
    owner text NOT NULL DEFAULT '',
    environment text NOT NULL DEFAULT 'DEV',
    criticality text NOT NULL DEFAULT 'Normal',
    repository text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS policies (
    id text PRIMARY KEY,
    name text NOT NULL UNIQUE,
    description text NOT NULL DEFAULT '',
    rules jsonb NOT NULL DEFAULT '{"paths":[]}'::jsonb,
    version integer NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS user_policies (
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    policy_id text NOT NULL REFERENCES policies(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, policy_id)
);

CREATE TABLE IF NOT EXISTS secrets (
    id text PRIMARY KEY,
    path text NOT NULL UNIQUE,
    description text NOT NULL DEFAULT '',
    application_id text REFERENCES applications(id) ON DELETE SET NULL,
    owner_user_id text REFERENCES users(id) ON DELETE SET NULL,
    tags jsonb NOT NULL DEFAULT '[]'::jsonb,
    risk_score integer NOT NULL DEFAULT 0 CHECK (risk_score BETWEEN 0 AND 100),
    current_version integer NOT NULL DEFAULT 0,
    created_by text NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);

CREATE INDEX IF NOT EXISTS secrets_path_idx ON secrets(path text_pattern_ops);
CREATE INDEX IF NOT EXISTS secrets_application_idx ON secrets(application_id) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS secret_versions (
    secret_id text NOT NULL REFERENCES secrets(id) ON DELETE CASCADE,
    version integer NOT NULL,
    ciphertext bytea NOT NULL,
    nonce bytea NOT NULL,
    encryption_key_id text NOT NULL REFERENCES user_keys(id) ON DELETE RESTRICT,
    key_version integer NOT NULL,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_by text NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    destroyed boolean NOT NULL DEFAULT false,
    PRIMARY KEY(secret_id, version)
);

CREATE TABLE IF NOT EXISTS settings (
    key text PRIMARY KEY,
    value_json jsonb,
    ciphertext bytea,
    nonce bytea,
    sensitive boolean NOT NULL DEFAULT false,
    updated_by text REFERENCES users(id) ON DELETE SET NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (
        (sensitive = false AND value_json IS NOT NULL AND ciphertext IS NULL AND nonce IS NULL)
        OR (sensitive = true AND value_json IS NULL AND ciphertext IS NOT NULL AND nonce IS NOT NULL)
    )
);

INSERT INTO settings(key, value_json, sensitive)
VALUES
    ('workflow', '{"approval_enabled":false,"targets":["secret_write","secret_delete"],"four_eyes":true,"required_approvals":1,"reviewer_role":"manager"}'::jsonb, false),
    ('service', '{"service_name":"jikim"}'::jsonb, false),
    ('oidc', '{"enabled":false}'::jsonb, false),
    ('ai', '{"enabled":false,"base_url":"","model":"","max_tokens":4096,"temperature":0.2,"timeout_seconds":600}'::jsonb, false)
    ,('security', '{"allow_local_login":true,"session_timeout_minutes":720,"require_password_change":false,"password_min_length":12,"audit_retention_days":180,"allowed_networks":""}'::jsonb, false)
    ,('notifications', '{"enabled":false}'::jsonb, false)
ON CONFLICT (key) DO NOTHING;

CREATE TABLE IF NOT EXISTS approval_requests (
    id text PRIMARY KEY,
    action text NOT NULL,
    resource text NOT NULL,
    requester_id text NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    approver_id text REFERENCES users(id) ON DELETE SET NULL,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
    payload_ciphertext bytea NOT NULL,
    payload_nonce bytea NOT NULL,
    comment text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    resolved_at timestamptz
);

CREATE INDEX IF NOT EXISTS approvals_status_created_idx ON approval_requests(status, created_at DESC);

CREATE TABLE IF NOT EXISTS transit_keys (
    name text PRIMARY KEY,
    version integer NOT NULL DEFAULT 1,
    encrypted_key bytea NOT NULL,
    nonce bytea NOT NULL,
    permissions jsonb NOT NULL DEFAULT '{"encrypt":true,"decrypt":true,"rotate":true,"manage":true}'::jsonb,
    created_by text NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS transit_key_versions (
    key_name text NOT NULL REFERENCES transit_keys(name) ON DELETE CASCADE,
    version integer NOT NULL,
    encrypted_key bytea NOT NULL,
    nonce bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(key_name, version)
);

CREATE TABLE IF NOT EXISTS audit_logs (
    id bigserial PRIMARY KEY,
    request_id text NOT NULL,
    user_id text REFERENCES users(id) ON DELETE SET NULL,
    username text NOT NULL DEFAULT '',
    action text NOT NULL,
    resource text NOT NULL DEFAULT '',
    method text NOT NULL,
    path text NOT NULL,
    status_code integer NOT NULL,
    success boolean NOT NULL,
    remote_ip text NOT NULL DEFAULT '',
    user_agent text NOT NULL DEFAULT '',
    details jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS audit_created_idx ON audit_logs(created_at DESC);
CREATE INDEX IF NOT EXISTS audit_user_idx ON audit_logs(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS audit_resource_idx ON audit_logs(resource, created_at DESC);
