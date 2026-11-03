-- Cada usuario registrado tiene un código corto y fijo (6 caracteres, empieza
-- con letra). Va al inicio del nombre de sus bases (abcd12-tienda), así dos
-- personas pueden llamar "tienda" a su base sin chocar.
ALTER TABLE client_users ADD COLUMN IF NOT EXISTS code TEXT;

-- Código a las cuentas que ya existen, sin repetir ninguno.
DO $$
DECLARE
    r RECORD;
    c TEXT;
BEGIN
    FOR r IN SELECT id FROM client_users WHERE code IS NULL LOOP
        LOOP
            c := chr(97 + floor(random() * 26)::int) || substr(md5(random()::text || clock_timestamp()::text || r.id::text), 1, 5);
            EXIT WHEN NOT EXISTS (SELECT 1 FROM client_users WHERE code = c);
        END LOOP;
        UPDATE client_users SET code = c WHERE id = r.id;
    END LOOP;
END $$;

ALTER TABLE client_users ALTER COLUMN code SET NOT NULL;
-- La app siempre manda su propio código; esto solo cubre inserciones directas.
ALTER TABLE client_users ALTER COLUMN code SET DEFAULT
    (chr(97 + floor(random() * 26)::int) || substr(md5(random()::text || clock_timestamp()::text), 1, 5));
CREATE UNIQUE INDEX IF NOT EXISTS client_users_code ON client_users (code);
