-- Per-database expiry control, internal session accounting and warnings.
ALTER TABLE client_databases
    ADD COLUMN IF NOT EXISTS never_expires     BOOLEAN     NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS max_idle_days     INT         CHECK (max_idle_days IS NULL OR max_idle_days > 0),
    ADD COLUMN IF NOT EXISTS grace_until       TIMESTAMPTZ,
    -- Sessions PosternDB itself opened (schema explorer, console, grants).
    -- Subtracted from pg_stat_database.sessions so browsing a database in
    -- the panel never counts as client activity.
    ADD COLUMN IF NOT EXISTS internal_sessions BIGINT      NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS internal_baseline BIGINT      NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS expiry_warned_at  TIMESTAMPTZ;

-- Local mode stores no hostname, so only real hostnames must be unique.
DROP INDEX IF EXISTS client_databases_hostname_active;
CREATE UNIQUE INDEX IF NOT EXISTS client_databases_hostname_active
    ON client_databases (hostname) WHERE state = 'active' AND hostname <> '';

-- Panel administrators besides the bootstrap ADMIN_USER from .env.
CREATE TABLE IF NOT EXISTS admins (
    id            BIGSERIAL PRIMARY KEY,
    username      TEXT        NOT NULL UNIQUE,
    password_hash TEXT        NOT NULL,
    role          TEXT        NOT NULL CHECK (role IN ('owner', 'admin', 'viewer')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by    TEXT        NOT NULL DEFAULT '',
    last_login_at TIMESTAMPTZ
);

-- Additional login roles attached to a client database.
CREATE TABLE IF NOT EXISTS database_users (
    id          BIGSERIAL PRIMARY KEY,
    database_id BIGINT      NOT NULL REFERENCES client_databases (id) ON DELETE CASCADE,
    name        TEXT        NOT NULL,
    role_name   TEXT        NOT NULL UNIQUE,
    access      TEXT        NOT NULL CHECK (access IN ('read', 'write')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (database_id, name)
);

-- Work queue for the backup sidecar (it has pg_dump; the app does not).
CREATE TABLE IF NOT EXISTS jobs (
    id           BIGSERIAL PRIMARY KEY,
    kind         TEXT        NOT NULL CHECK (kind IN ('dump', 'archive', 'restore', 'clone', 'delete_file')),
    status       TEXT        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'done', 'failed')),
    slug         TEXT        NOT NULL DEFAULT '',
    source_db    TEXT        NOT NULL DEFAULT '',
    target_db    TEXT        NOT NULL DEFAULT '',
    target_role  TEXT        NOT NULL DEFAULT '',
    file         TEXT        NOT NULL DEFAULT '',
    bytes        BIGINT,
    reason       TEXT        NOT NULL DEFAULT '',
    error        TEXT        NOT NULL DEFAULT '',
    requested_by TEXT        NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at   TIMESTAMPTZ,
    finished_at  TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS jobs_pending ON jobs (id) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS jobs_slug ON jobs (slug, created_at DESC);

-- Reusable SQL to initialise new databases.
CREATE TABLE IF NOT EXISTS templates (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT        NOT NULL UNIQUE,
    description TEXT        NOT NULL DEFAULT '',
    sql         TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO templates (name, description, sql) VALUES (
    'Contactos (demo)',
    'Tabla de contactos con datos de ejemplo, útil para probar una conexión.',
    $tpl$CREATE TABLE contactos (
    id        BIGSERIAL PRIMARY KEY,
    nombre    TEXT        NOT NULL,
    email     TEXT,
    telefono  TEXT,
    creado_en TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO contactos (nombre, email, telefono) VALUES
    ('Ana López', 'ana@example.com', '5512345678'),
    ('Bruno Díaz', 'bruno@example.com', NULL);$tpl$
) ON CONFLICT (name) DO NOTHING;
