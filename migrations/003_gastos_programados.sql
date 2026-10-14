-- Gastos con vencimiento futuro y apartados virtuales asociados a cada ciclo.

CREATE UNIQUE INDEX IF NOT EXISTS uq_categorias_id_usuario
    ON categorias (id, usuario_id);

CREATE TABLE IF NOT EXISTS gastos_programados (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    usuario_id          UUID NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
    categoria_id        UUID,
    cuenta_id           UUID,
    nombre              TEXT NOT NULL CHECK (length(trim(nombre)) BETWEEN 1 AND 100),
    monto               NUMERIC(12,2) NOT NULL CHECK (monto > 0),
    fecha_proximo_pago  DATE NOT NULL,
    dia_vencimiento     SMALLINT NOT NULL CHECK (dia_vencimiento BETWEEN 1 AND 31),
    frecuencia          TEXT NOT NULL CHECK (frecuencia IN ('unico', 'mensual', 'trimestral', 'semestral', 'anual')),
    activo              BOOLEAN NOT NULL DEFAULT true,
    creado_en           TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (id, usuario_id),
    CONSTRAINT fk_programados_categoria_usuario
        FOREIGN KEY (categoria_id, usuario_id) REFERENCES categorias(id, usuario_id)
        ON DELETE SET NULL (categoria_id),
    CONSTRAINT fk_programados_cuenta_usuario
        FOREIGN KEY (cuenta_id, usuario_id) REFERENCES cuentas(id, usuario_id)
        ON DELETE SET NULL (cuenta_id)
);

CREATE TABLE IF NOT EXISTS programado_apartados (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    programado_id       UUID NOT NULL,
    usuario_id          UUID NOT NULL,
    fecha_programada    DATE NOT NULL,
    monto               NUMERIC(12,2) NOT NULL CHECK (monto > 0),
    fecha               DATE NOT NULL DEFAULT CURRENT_DATE,
    creado_en           TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT fk_apartados_programado_usuario
        FOREIGN KEY (programado_id, usuario_id) REFERENCES gastos_programados(id, usuario_id)
        ON DELETE CASCADE
);

ALTER TABLE gastos
    ADD COLUMN IF NOT EXISTS programado_id UUID,
    ADD COLUMN IF NOT EXISTS programado_fecha DATE;

ALTER TABLE gastos
    DROP CONSTRAINT IF EXISTS fk_gastos_programado_usuario;
ALTER TABLE gastos
    ADD CONSTRAINT fk_gastos_programado_usuario
    FOREIGN KEY (programado_id, usuario_id) REFERENCES gastos_programados(id, usuario_id)
    ON DELETE SET NULL (programado_id);

CREATE INDEX IF NOT EXISTS idx_programados_usuario_activo_fecha
    ON gastos_programados (usuario_id, activo, fecha_proximo_pago);
CREATE INDEX IF NOT EXISTS idx_apartados_programado_ciclo
    ON programado_apartados (programado_id, fecha_programada, fecha);
CREATE UNIQUE INDEX IF NOT EXISTS uq_gastos_programado_fecha
    ON gastos (programado_id, programado_fecha) WHERE programado_id IS NOT NULL;
