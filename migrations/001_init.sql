-- Esquema que espera esta API. Ejecútalo en el SQL Editor de Neon.
-- Si ya creaste tu propio esquema, compara nombres de tablas/columnas con este.

CREATE TABLE IF NOT EXISTS usuarios (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email         TEXT NOT NULL UNIQUE,
    nombre        TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    creado_en     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS categorias (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    usuario_id UUID NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
    nombre     TEXT NOT NULL,
    tipo       TEXT NOT NULL CHECK (tipo IN ('gasto', 'ingreso')),
    UNIQUE (usuario_id, nombre, tipo)
);

CREATE TABLE IF NOT EXISTS ingresos (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    usuario_id   UUID NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
    categoria_id UUID REFERENCES categorias(id) ON DELETE SET NULL,
    descripcion  TEXT NOT NULL,
    monto        NUMERIC(12,2) NOT NULL CHECK (monto > 0),
    fecha        DATE NOT NULL,
    periodicidad TEXT NOT NULL DEFAULT 'unico'
                 CHECK (periodicidad IN ('unico', 'semanal', 'quincenal', 'mensual')),
    creado_en    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS gastos_recurrentes (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    usuario_id      UUID NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
    categoria_id    UUID REFERENCES categorias(id) ON DELETE SET NULL,
    nombre          TEXT NOT NULL,
    monto           NUMERIC(12,2) NOT NULL CHECK (monto > 0),
    dia_vencimiento SMALLINT NOT NULL CHECK (dia_vencimiento BETWEEN 1 AND 31),
    activo          BOOLEAN NOT NULL DEFAULT true,
    creado_en       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS gastos (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    usuario_id    UUID NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
    categoria_id  UUID REFERENCES categorias(id) ON DELETE SET NULL,
    recurrente_id UUID REFERENCES gastos_recurrentes(id) ON DELETE SET NULL,
    descripcion   TEXT NOT NULL,
    monto         NUMERIC(12,2) NOT NULL CHECK (monto > 0),
    fecha         DATE NOT NULL,
    creado_en     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_gastos_usuario_fecha   ON gastos (usuario_id, fecha);
CREATE INDEX IF NOT EXISTS idx_ingresos_usuario_fecha ON ingresos (usuario_id, fecha);

-- Evita duplicar el gasto de un servicio recurrente en la misma fecha (generación idempotente).
CREATE UNIQUE INDEX IF NOT EXISTS uq_gastos_recurrente_fecha
    ON gastos (recurrente_id, fecha) WHERE recurrente_id IS NOT NULL;
