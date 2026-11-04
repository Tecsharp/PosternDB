-- Administradores y usuarios pasan a ser lo mismo: una cuenta con un rol.
--   user   = usuario normal (su panel reducido)
--   viewer = administrador de solo lectura
--   admin  = administrador
-- El propietario sigue siendo el usuario de ADMIN_USER en el .env: no es una
-- fila de esta tabla, así que ningún administrador puede tocarlo.
ALTER TABLE client_users
    ADD COLUMN IF NOT EXISTS role TEXT NOT NULL DEFAULT 'user' CHECK (role IN ('user', 'viewer', 'admin'));

-- Los administradores que ya existían se convierten en cuentas con ese rol
-- (un "owner" guardado pasa a admin). Si ya había un usuario con el mismo
-- nombre de usuario, es la misma persona: se le da el rol y conserva su
-- contraseña (la del administrador solo se usa si no tenía).
DO $$
DECLARE
    a      RECORD;
    mapped TEXT;
    c      TEXT;
BEGIN
    FOR a IN SELECT * FROM admins ORDER BY id LOOP
        mapped := CASE WHEN a.role = 'viewer' THEN 'viewer' ELSE 'admin' END;
        UPDATE client_users
           SET role = mapped, password_hash = COALESCE(NULLIF(password_hash, ''), a.password_hash), updated_at = now()
         WHERE username = a.username;
        IF FOUND THEN
            -- Matching only by username cannot prove it is the same person, so
            -- leave a trace the owner can check in Actividad and in the log.
            RAISE NOTICE 'user % merged with the administrator of the same name (role %)', a.username, mapped;
            INSERT INTO audit_log (actor, action, target, details)
            VALUES ('migration-010', 'role_changed', a.username, jsonb_build_object('from', 'user', 'to', mapped, 'merged', true));
        ELSE
            LOOP
                c := chr(97 + floor(random() * 26)::int) || substr(md5(random()::text || clock_timestamp()::text || a.id::text), 1, 5);
                EXIT WHEN NOT EXISTS (SELECT 1 FROM client_users WHERE code = c);
            END LOOP;
            INSERT INTO client_users (username, password_hash, full_name, role, created_by, created_at, last_login_at, code)
            VALUES (a.username, a.password_hash, '', mapped, a.created_by, a.created_at, a.last_login_at, c);
        END IF;
    END LOOP;
END $$;

-- Se conserva por si hace falta consultar los datos originales; la app ya no la usa.
ALTER TABLE admins RENAME TO admins_migrated;
