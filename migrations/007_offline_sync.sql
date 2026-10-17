-- Idempotency receipts and deletion tombstones for offline clients.
CREATE TABLE IF NOT EXISTS sync_operations (
    usuario_id UUID NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
    op_id UUID NOT NULL,
    fingerprint TEXT NOT NULL,
    result JSONB NOT NULL,
    creado_en TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (usuario_id, op_id)
);

CREATE TABLE IF NOT EXISTS sync_tombstones (
    id BIGSERIAL PRIMARY KEY,
    usuario_id UUID NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
    entidad TEXT NOT NULL,
    entidad_id UUID NOT NULL,
    eliminado_en TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (usuario_id, entidad, entidad_id)
);
CREATE INDEX IF NOT EXISTS idx_sync_tombstones_usuario
    ON sync_tombstones (usuario_id, id);

CREATE OR REPLACE FUNCTION registrar_borrado_sync() RETURNS trigger AS $$
DECLARE
    entidad_sync TEXT := TG_ARGV[0];
    id_usuario UUID := OLD.usuario_id;
    id_entidad UUID := OLD.id;
BEGIN
    IF TG_TABLE_NAME = 'gastos' AND OLD.programado_id IS NOT NULL THEN
        INSERT INTO sync_tombstones (usuario_id, entidad, entidad_id)
        VALUES (id_usuario, 'scheduled_expense_payments', id_entidad)
        ON CONFLICT (usuario_id, entidad, entidad_id)
        DO UPDATE SET eliminado_en = now();
    END IF;

    INSERT INTO sync_tombstones (usuario_id, entidad, entidad_id)
    VALUES (id_usuario, entidad_sync, id_entidad)
    ON CONFLICT (usuario_id, entidad, entidad_id)
    DO UPDATE SET eliminado_en = now();
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION limpiar_borrado_sync() RETURNS trigger AS $$
DECLARE
    entidad_sync TEXT := TG_ARGV[0];
BEGIN
    IF TG_TABLE_NAME = 'gastos' AND NEW.programado_id IS NOT NULL THEN
        DELETE FROM sync_tombstones
        WHERE usuario_id = NEW.usuario_id
          AND entidad = 'scheduled_expense_payments'
          AND entidad_id = NEW.id;
    END IF;
    DELETE FROM sync_tombstones
    WHERE usuario_id = NEW.usuario_id
      AND entidad = entidad_sync
      AND entidad_id = NEW.id;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DO $$
DECLARE
    tabla TEXT;
    entidad TEXT;
BEGIN
    FOR tabla, entidad IN
        SELECT * FROM (VALUES
            ('categorias', 'categories'),
            ('cuentas', 'accounts'),
            ('gastos', 'expenses'),
            ('ingresos', 'incomes'),
            ('transferencias', 'transfers'),
            ('gastos_recurrentes', 'recurring_services'),
            ('gastos_programados', 'scheduled_expenses'),
            ('programado_apartados', 'scheduled_expense_allocations'),
            ('ingresos_programados', 'scheduled_incomes'),
            ('asignaciones_ingresos_programados', 'scheduled_income_assignments'),
            ('ajustes_saldo', 'balance_adjustments')
        ) AS entidades(tabla, entidad)
    LOOP
        EXECUTE format('DROP TRIGGER IF EXISTS sync_tombstone_delete ON %I', tabla);
        EXECUTE format(
            'CREATE TRIGGER sync_tombstone_delete AFTER DELETE ON %I FOR EACH ROW EXECUTE FUNCTION registrar_borrado_sync(%L)',
            tabla, entidad
        );
        EXECUTE format('DROP TRIGGER IF EXISTS sync_tombstone_insert ON %I', tabla);
        EXECUTE format(
            'CREATE TRIGGER sync_tombstone_insert AFTER INSERT ON %I FOR EACH ROW EXECUTE FUNCTION limpiar_borrado_sync(%L)',
            tabla, entidad
        );
    END LOOP;
END;
$$;
