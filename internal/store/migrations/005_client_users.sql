-- Replaces the per-administrator exception from migration 004: expiration
-- exceptions belong to the self-service client who owns the database, not
-- to whichever panel administrator happened to create it.
DROP TABLE IF EXISTS admin_expiry_overrides;
ALTER TABLE client_databases DROP COLUMN IF EXISTS owner_admin;

-- client_users are tenants of the "panel de usuario" (never the admin
-- panel): they register themselves, or the owner adds them directly, and
-- they see and manage only their own databases (internal/clients, the
-- /api/auth/register endpoint and the /api/me/* routes).
CREATE TABLE IF NOT EXISTS client_users (
    id            BIGSERIAL PRIMARY KEY,
    username      TEXT        NOT NULL UNIQUE,
    password_hash TEXT        NOT NULL,
    never_expires BOOLEAN     NOT NULL DEFAULT false,
    max_idle_days INT         CHECK (max_idle_days IS NULL OR max_idle_days > 0),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- The admin username that created this client directly, or '' when the
    -- client registered themselves through the public sign-up form.
    created_by    TEXT        NOT NULL DEFAULT '',
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by    TEXT        NOT NULL DEFAULT '',
    last_login_at TIMESTAMPTZ
);

ALTER TABLE client_databases
    ADD COLUMN IF NOT EXISTS client_user_id BIGINT REFERENCES client_users (id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS client_databases_client_user ON client_databases (client_user_id);
