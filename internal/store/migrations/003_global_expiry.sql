-- Global inactivity policy, overriding DB_EXPIRATION_DAYS / EXPIRING_SOON_DAYS
-- from .env at runtime. NULL means "use the .env value"; only the owner
-- (ADMIN_USER) can change this, from Ajustes -> Expiración in the panel.
CREATE TABLE IF NOT EXISTS platform_settings (
    id                 SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    expiration_days    INT CHECK (expiration_days IS NULL OR expiration_days > 0),
    expiring_soon_days INT CHECK (expiring_soon_days IS NULL OR expiring_soon_days > 0),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by         TEXT NOT NULL DEFAULT ''
);

INSERT INTO platform_settings (id) VALUES (1) ON CONFLICT (id) DO NOTHING;
