CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id text PRIMARY KEY,
    event_type text NOT NULL,
    resource text NOT NULL DEFAULT '',
    actor_id text REFERENCES users(id) ON DELETE SET NULL,
    request_id text NOT NULL DEFAULT '',
    payload jsonb NOT NULL,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'delivered', 'failed')),
    attempt_count integer NOT NULL DEFAULT 0,
    response_status integer,
    last_error text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    last_attempt_at timestamptz,
    delivered_at timestamptz
);

CREATE INDEX IF NOT EXISTS webhook_deliveries_status_created_idx
    ON webhook_deliveries(status, created_at DESC);
