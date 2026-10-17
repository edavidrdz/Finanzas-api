ALTER TABLE gastos
    ADD COLUMN IF NOT EXISTS ubicacion_latitud DOUBLE PRECISION,
    ADD COLUMN IF NOT EXISTS ubicacion_longitud DOUBLE PRECISION,
    ADD COLUMN IF NOT EXISTS ubicacion_nombre TEXT;

ALTER TABLE gastos
    DROP CONSTRAINT IF EXISTS ck_gastos_ubicacion;
ALTER TABLE gastos
    ADD CONSTRAINT ck_gastos_ubicacion CHECK (
        (ubicacion_latitud IS NULL AND ubicacion_longitud IS NULL)
        OR (
            ubicacion_latitud BETWEEN -90 AND 90
            AND ubicacion_longitud BETWEEN -180 AND 180
        )
    );

CREATE TABLE IF NOT EXISTS gastos_pendientes (
    id                  UUID PRIMARY KEY,
    usuario_id          UUID NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
    categoria_id        UUID REFERENCES categorias(id) ON DELETE SET NULL,
    cuenta_id           UUID REFERENCES cuentas(id) ON DELETE SET NULL,
    descripcion         TEXT NOT NULL DEFAULT '' CHECK (length(descripcion) <= 200),
    monto               NUMERIC(12,2) CHECK (monto IS NULL OR monto > 0),
    fecha               DATE NOT NULL,
    ubicacion_latitud   DOUBLE PRECISION,
    ubicacion_longitud  DOUBLE PRECISION,
    ubicacion_nombre    TEXT,
    iniciada_en         TIMESTAMPTZ NOT NULL,
    completando         BOOLEAN NOT NULL DEFAULT false,
    creado_en           TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (
        (ubicacion_latitud IS NULL AND ubicacion_longitud IS NULL)
        OR (
            ubicacion_latitud BETWEEN -90 AND 90
            AND ubicacion_longitud BETWEEN -180 AND 180
        )
    )
);

CREATE INDEX IF NOT EXISTS idx_gastos_pendientes_usuario_fecha
    ON gastos_pendientes (usuario_id, fecha DESC);

DROP TRIGGER IF EXISTS sync_tombstone_delete ON gastos_pendientes;
CREATE TRIGGER sync_tombstone_delete AFTER DELETE ON gastos_pendientes
    FOR EACH ROW EXECUTE FUNCTION registrar_borrado_sync('expense_drafts');
DROP TRIGGER IF EXISTS sync_tombstone_insert ON gastos_pendientes;
CREATE TRIGGER sync_tombstone_insert AFTER INSERT ON gastos_pendientes
    FOR EACH ROW EXECUTE FUNCTION limpiar_borrado_sync('expense_drafts');
