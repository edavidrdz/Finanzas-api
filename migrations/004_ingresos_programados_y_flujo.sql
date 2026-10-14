-- Ingresos futuros, asignaciones a gastos y ajustes para conciliar saldos existentes.

CREATE TABLE IF NOT EXISTS ingresos_programados (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    usuario_id              UUID NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
    categoria_id            UUID,
    cuenta_id               UUID,
    nombre                  TEXT NOT NULL CHECK (length(trim(nombre)) BETWEEN 1 AND 100),
    monto                   NUMERIC(12,2) NOT NULL CHECK (monto > 0),
    fecha_proximo_ingreso   DATE NOT NULL,
    dia_ingreso             SMALLINT NOT NULL CHECK (dia_ingreso BETWEEN 1 AND 31),
    frecuencia              TEXT NOT NULL CHECK (frecuencia IN ('unico', 'semanal', 'quincenal', 'mensual', 'trimestral', 'semestral', 'anual')),
    certeza                 TEXT NOT NULL DEFAULT 'estimado' CHECK (certeza IN ('estimado', 'confirmado')),
    activo                  BOOLEAN NOT NULL DEFAULT true,
    creado_en               TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (id, usuario_id),
    CONSTRAINT fk_ingresos_programados_categoria_usuario
        FOREIGN KEY (categoria_id, usuario_id) REFERENCES categorias(id, usuario_id)
        ON DELETE SET NULL (categoria_id),
    CONSTRAINT fk_ingresos_programados_cuenta_usuario
        FOREIGN KEY (cuenta_id, usuario_id) REFERENCES cuentas(id, usuario_id)
        ON DELETE SET NULL (cuenta_id)
);

ALTER TABLE ingresos
    ADD COLUMN IF NOT EXISTS ingreso_programado_id UUID,
    ADD COLUMN IF NOT EXISTS programado_fecha DATE;

ALTER TABLE ingresos
    DROP CONSTRAINT IF EXISTS fk_ingresos_programado_usuario;
ALTER TABLE ingresos
    ADD CONSTRAINT fk_ingresos_programado_usuario
    FOREIGN KEY (ingreso_programado_id, usuario_id) REFERENCES ingresos_programados(id, usuario_id)
    ON DELETE SET NULL (ingreso_programado_id);

CREATE UNIQUE INDEX IF NOT EXISTS uq_ingresos_programado_fecha
    ON ingresos (ingreso_programado_id, programado_fecha) WHERE ingreso_programado_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS asignaciones_ingresos_programados (
    id                          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    usuario_id                  UUID NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
    ingreso_programado_id       UUID NOT NULL,
    fecha_ingreso_programada    DATE NOT NULL,
    gasto_programado_id         UUID NOT NULL,
    fecha_gasto_programada      DATE NOT NULL,
    monto                       NUMERIC(12,2) NOT NULL CHECK (monto > 0),
    creado_en                   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT fk_asignacion_ingreso_usuario
        FOREIGN KEY (ingreso_programado_id, usuario_id) REFERENCES ingresos_programados(id, usuario_id)
        ON DELETE CASCADE,
    CONSTRAINT fk_asignacion_gasto_usuario
        FOREIGN KEY (gasto_programado_id, usuario_id) REFERENCES gastos_programados(id, usuario_id)
        ON DELETE CASCADE,
    CONSTRAINT uq_asignacion_ingreso_gasto_ciclo
        UNIQUE (ingreso_programado_id, fecha_ingreso_programada, gasto_programado_id, fecha_gasto_programada)
);

ALTER TABLE programado_apartados
    ADD COLUMN IF NOT EXISTS ingreso_programado_id UUID,
    ADD COLUMN IF NOT EXISTS ingreso_fecha_programada DATE;
ALTER TABLE programado_apartados
    DROP CONSTRAINT IF EXISTS fk_apartado_ingreso_programado;
ALTER TABLE programado_apartados
    ADD CONSTRAINT fk_apartado_ingreso_programado
    FOREIGN KEY (ingreso_programado_id, usuario_id)
    REFERENCES ingresos_programados(id, usuario_id)
    ON DELETE SET NULL (ingreso_programado_id);

CREATE INDEX IF NOT EXISTS idx_ingresos_programados_usuario_activo_fecha
    ON ingresos_programados (usuario_id, activo, fecha_proximo_ingreso);
CREATE INDEX IF NOT EXISTS idx_asignaciones_ingreso_ciclo
    ON asignaciones_ingresos_programados (ingreso_programado_id, fecha_ingreso_programada);
CREATE INDEX IF NOT EXISTS idx_asignaciones_gasto_ciclo
    ON asignaciones_ingresos_programados (gasto_programado_id, fecha_gasto_programada);
CREATE INDEX IF NOT EXISTS idx_apartados_ingreso_fuente
    ON programado_apartados (ingreso_programado_id, ingreso_fecha_programada);

CREATE TABLE IF NOT EXISTS ajustes_saldo (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    usuario_id  UUID NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
    cuenta_id   UUID NOT NULL,
    monto       NUMERIC(12,2) NOT NULL CHECK (monto <> 0),
    fecha       DATE NOT NULL,
    motivo      TEXT NOT NULL DEFAULT 'Conciliación de saldo' CHECK (length(trim(motivo)) BETWEEN 1 AND 200),
    creado_en   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT fk_ajuste_cuenta_usuario
        FOREIGN KEY (cuenta_id, usuario_id) REFERENCES cuentas(id, usuario_id)
        ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_ajustes_cuenta_fecha
    ON ajustes_saldo (cuenta_id, fecha);
