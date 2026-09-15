CREATE TABLE IF NOT EXISTS mail_deliveries (
    id text PRIMARY KEY,
    event text NOT NULL,
    recipient text NOT NULL,
    subject text NOT NULL DEFAULT '',
    resource text NOT NULL DEFAULT '',
    actor_id text REFERENCES users(id) ON DELETE SET NULL,
    status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'sent', 'failed')),
    attempts integer NOT NULL DEFAULT 0,
    error_message text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS mail_deliveries_status_created_idx
    ON mail_deliveries(status, created_at DESC);
