-- Cuentas financieras y transferencias (incluye retiros de efectivo y pagos de tarjeta).
-- Los movimientos anteriores al saldo inicial conservan cuenta_id NULL.

CREATE TABLE IF NOT EXISTS cuentas (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    usuario_id           UUID NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
    nombre               TEXT NOT NULL CHECK (length(trim(nombre)) BETWEEN 1 AND 100),
    institucion          TEXT,
    tipo                 TEXT NOT NULL CHECK (tipo IN ('efectivo', 'debito', 'credito', 'ahorro')),
    saldo_inicial        NUMERIC(12,2) NOT NULL DEFAULT 0 CHECK (saldo_inicial >= 0),
    fecha_saldo_inicial  DATE NOT NULL DEFAULT CURRENT_DATE,
    limite_credito       NUMERIC(12,2) CHECK (limite_credito IS NULL OR limite_credito > 0),
    activa               BOOLEAN NOT NULL DEFAULT true,
    creado_en            TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (tipo = 'credito' OR limite_credito IS NULL),
    UNIQUE (id, usuario_id)
);

ALTER TABLE gastos
    ADD COLUMN IF NOT EXISTS cuenta_id UUID;

ALTER TABLE ingresos
    ADD COLUMN IF NOT EXISTS cuenta_id UUID;

ALTER TABLE gastos_recurrentes
    ADD COLUMN IF NOT EXISTS cuenta_id UUID;

CREATE TABLE IF NOT EXISTS transferencias (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    usuario_id         UUID NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
    cuenta_origen_id   UUID NOT NULL REFERENCES cuentas(id) ON DELETE RESTRICT,
    cuenta_destino_id  UUID NOT NULL REFERENCES cuentas(id) ON DELETE RESTRICT,
    descripcion        TEXT NOT NULL DEFAULT 'Transferencia',
    monto              NUMERIC(12,2) NOT NULL CHECK (monto > 0),
    fecha              DATE NOT NULL,
    creado_en          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (cuenta_origen_id <> cuenta_destino_id)
);

CREATE INDEX IF NOT EXISTS idx_cuentas_usuario_activa
    ON cuentas (usuario_id, activa, nombre);
CREATE INDEX IF NOT EXISTS idx_gastos_usuario_cuenta_fecha
    ON gastos (usuario_id, cuenta_id, fecha);
CREATE INDEX IF NOT EXISTS idx_ingresos_usuario_cuenta_fecha
    ON ingresos (usuario_id, cuenta_id, fecha);
CREATE INDEX IF NOT EXISTS idx_recurrentes_usuario_cuenta
    ON gastos_recurrentes (usuario_id, cuenta_id);
CREATE INDEX IF NOT EXISTS idx_transferencias_usuario_fecha
    ON transferencias (usuario_id, fecha);

-- Impide relacionar una cuenta de otro usuario, también ante escrituras fuera de la API.
ALTER TABLE gastos
    DROP CONSTRAINT IF EXISTS fk_gastos_cuenta_usuario;
ALTER TABLE gastos
    ADD CONSTRAINT fk_gastos_cuenta_usuario
    FOREIGN KEY (cuenta_id, usuario_id) REFERENCES cuentas(id, usuario_id)
    ON DELETE SET NULL (cuenta_id);

ALTER TABLE ingresos
    DROP CONSTRAINT IF EXISTS fk_ingresos_cuenta_usuario;
ALTER TABLE ingresos
    ADD CONSTRAINT fk_ingresos_cuenta_usuario
    FOREIGN KEY (cuenta_id, usuario_id) REFERENCES cuentas(id, usuario_id)
    ON DELETE SET NULL (cuenta_id);

ALTER TABLE gastos_recurrentes
    DROP CONSTRAINT IF EXISTS fk_recurrentes_cuenta_usuario;
ALTER TABLE gastos_recurrentes
    ADD CONSTRAINT fk_recurrentes_cuenta_usuario
    FOREIGN KEY (cuenta_id, usuario_id) REFERENCES cuentas(id, usuario_id)
    ON DELETE SET NULL (cuenta_id);

ALTER TABLE transferencias
    DROP CONSTRAINT IF EXISTS fk_transferencias_origen_usuario;
ALTER TABLE transferencias
    ADD CONSTRAINT fk_transferencias_origen_usuario
    FOREIGN KEY (cuenta_origen_id, usuario_id) REFERENCES cuentas(id, usuario_id)
    ON DELETE RESTRICT;
ALTER TABLE transferencias
    DROP CONSTRAINT IF EXISTS fk_transferencias_destino_usuario;
ALTER TABLE transferencias
    ADD CONSTRAINT fk_transferencias_destino_usuario
    FOREIGN KEY (cuenta_destino_id, usuario_id) REFERENCES cuentas(id, usuario_id)
    ON DELETE RESTRICT;
