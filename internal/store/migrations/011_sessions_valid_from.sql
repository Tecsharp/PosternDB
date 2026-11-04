-- Sessions issued before this moment stop working. It moves forward whenever
-- the account's password changes or is reset, so a stolen session cookie does
-- not survive the new password. 'epoch' keeps every current session valid.
ALTER TABLE client_users
    ADD COLUMN IF NOT EXISTS sessions_valid_from TIMESTAMPTZ NOT NULL DEFAULT 'epoch';
