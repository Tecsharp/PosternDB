-- Metadata for every client database ever provisioned.
CREATE TABLE IF NOT EXISTS client_databases (
    id                BIGSERIAL PRIMARY KEY,
    slug              TEXT        NOT NULL,
    db_name           TEXT        NOT NULL,
    role_name         TEXT        NOT NULL,
    hostname          TEXT        NOT NULL,
    state             TEXT        NOT NULL DEFAULT 'active'
                      CHECK (state IN ('active', 'expired', 'deleted')),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    sessions_baseline BIGINT      NOT NULL DEFAULT 0,
    expired_at        TIMESTAMPTZ,
    deleted_at        TIMESTAMPTZ,
    archived_as       TEXT
);

-- A slug can be reused only after the previous database was removed from the
-- panel; expired rows still block it so the operator notices the history.
CREATE UNIQUE INDEX IF NOT EXISTS client_databases_slug_live
    ON client_databases (slug) WHERE state <> 'deleted';

CREATE UNIQUE INDEX IF NOT EXISTS client_databases_hostname_active
    ON client_databases (hostname) WHERE state = 'active';

CREATE TABLE IF NOT EXISTS audit_log (
    id          BIGSERIAL PRIMARY KEY,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    actor       TEXT        NOT NULL,
    action      TEXT        NOT NULL,
    target      TEXT        NOT NULL DEFAULT '',
    details     JSONB       NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS audit_log_occurred_at ON audit_log (occurred_at DESC);
