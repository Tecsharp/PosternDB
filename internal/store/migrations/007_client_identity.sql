-- Cuentas de usuario con correo, nombre completo e inicio de sesión con
-- Google. Una cuenta puede tener contraseña, vínculo con Google, o ambos:
-- por eso password_hash deja de ser obligatorio.
ALTER TABLE client_users
    ADD COLUMN IF NOT EXISTS email             TEXT,
    ADD COLUMN IF NOT EXISTS full_name         TEXT        NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS google_sub        TEXT,
    ADD COLUMN IF NOT EXISTS email_verified_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS email_use_accepted_at TIMESTAMPTZ;

ALTER TABLE client_users ALTER COLUMN password_hash DROP NOT NULL;

-- Un correo = una cuenta (sin distinguir mayúsculas), y una cuenta de Google
-- solo puede estar vinculada a un usuario.
CREATE UNIQUE INDEX IF NOT EXISTS client_users_email ON client_users (lower(email)) WHERE email IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS client_users_google_sub ON client_users (google_sub) WHERE google_sub IS NOT NULL;
