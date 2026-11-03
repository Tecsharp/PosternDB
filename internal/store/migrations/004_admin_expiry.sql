-- Per-admin inactivity exceptions. "Usuario2 y usuario3 se registraron, pero
-- usuario3 nunca expira": the owner sets this once per admin and it applies
-- to every database that admin creates, without touching each one.
--
-- Precedence (resolved in Go, see expiry.ApplyOwnerOverride):
--   1. The database's own exception (never_expires / max_idle_days), if set.
--   2. Its creator's admin-level exception, if the creator has one.
--   3. The global policy (platform_settings, falling back to .env).

ALTER TABLE client_databases
    ADD COLUMN IF NOT EXISTS owner_admin TEXT NOT NULL DEFAULT '';

-- A row only exists for an admin that has an override; no row means
-- "follows the global policy". No foreign key to admins: the bootstrap
-- owner (ADMIN_USER) is not stored there, and a stale username is simply
-- inert (matches owner_admin, which has no foreign key either).
CREATE TABLE IF NOT EXISTS admin_expiry_overrides (
    admin_username TEXT PRIMARY KEY,
    never_expires  BOOLEAN NOT NULL DEFAULT false,
    max_idle_days  INT CHECK (max_idle_days IS NULL OR max_idle_days > 0),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by     TEXT NOT NULL DEFAULT ''
);
