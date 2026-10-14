CREATE TABLE IF NOT EXISTS password_reset_tokens (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    usuario_id UUID NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    expira_en  TIMESTAMPTZ NOT NULL,
    usado_en   TIMESTAMPTZ,
    creado_en  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_password_reset_tokens_usuario_creado
    ON password_reset_tokens (usuario_id, creado_en DESC);
