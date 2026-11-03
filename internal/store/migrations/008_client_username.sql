-- Las cuentas creadas con Google empiezan con el correo como usuario y deben
-- elegir un nombre de usuario la primera vez que entran.
ALTER TABLE client_users ADD COLUMN IF NOT EXISTS needs_username BOOLEAN NOT NULL DEFAULT false;
