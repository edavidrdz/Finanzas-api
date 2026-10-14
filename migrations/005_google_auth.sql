-- Permite cuentas sin contraseña local y vincula identidades OAuth verificadas.

ALTER TABLE usuarios
    ALTER COLUMN password_hash DROP NOT NULL;

CREATE TABLE IF NOT EXISTS auth_identities (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    usuario_id       UUID NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
    provider         TEXT NOT NULL CHECK (provider IN ('google')),
    provider_subject TEXT NOT NULL,
    email            TEXT NOT NULL,
    creado_en        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, provider_subject),
    UNIQUE (usuario_id, provider)
);
