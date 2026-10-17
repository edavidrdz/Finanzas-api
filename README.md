# finanzas-api

API en Go para una app de finanzas personales: gastos diarios, ingresos (nómina),
servicios del hogar recurrentes (luz, internet, gasolina...) y resumen mensual.

**Stack:** Go 1.22+ · chi · pgx (PostgreSQL en Neon) · JWT · bcrypt · Render.

## Estructura

```
cmd/api/main.go            punto de entrada y rutas
internal/config            variables de entorno
internal/db                pool de conexiones (Neon)
internal/auth              JWT, contraseñas y middleware
internal/httpx             respuestas JSON y manejo de errores de BD
internal/handlers          endpoints (auth, categorias, cuentas, transferencias y movimientos)
migrations/                  scripts SQL versionados
```

## Primer arranque

```bash
go mod tidy                 # descarga dependencias y genera go.sum (súbelo a GitHub)
cp .env.example .env        # edita DATABASE_URL y JWT_SECRET
export $(grep -v '^#' .env | xargs)
go run ./cmd/api
```

Verifica: `curl localhost:8080/health/db`

Al actualizar una instalación existente, ejecuta en el SQL Editor de Neon, en orden:

1. `migrations/001_init.sql` (solo necesario si aún no se creó el esquema base).
2. `migrations/002_cuentas_y_transferencias.sql`.
3. `migrations/003_gastos_programados.sql`.
4. `migrations/004_ingresos_programados_y_flujo.sql`.
5. `migrations/005_google_auth.sql` para permitir acceso y registro con Google.
6. `migrations/006_password_reset.sql` para habilitar recuperación segura de contraseña.
7. `migrations/007_offline_sync.sql` para recibos idempotentes de operaciones offline y tombstones de borrados remotos.

La segunda migración agrega cuentas financieras y transferencias sin borrar los gastos
ni ingresos existentes. Los movimientos anteriores quedan sin cuenta asignada y no se
incluyen en el saldo calculado desde la fecha de saldo inicial de cada cuenta.

La tercera migración agrega gastos programados y apartados virtuales. Los apartados se
vinculan al vencimiento actual y se conservan como historial. Al confirmar un pago se
registra un gasto normal, se avanza el siguiente vencimiento (o se desactiva si era único)
y no se realizan transferencias automáticas.

La cuarta migración permite registrar ingresos esperados, asignarlos a vencimientos futuros
y guardar ajustes de conciliación de saldos sin alterar los totales históricos de ingresos
y gastos. Cuando se confirma un ingreso esperado, se crea el ingreso real y sus asignaciones
vigentes se convierten en apartados virtuales del gasto. El flujo quincenal usa ingresos
reales marcados como `quincenal`; resta gastos de cuentas no crediticias y transferencias
de pago de tarjeta, pero presenta las compras con tarjeta por separado para no duplicarlas.

La quinta migración permite usuarios autenticados por Google sin contraseña local y registra
vínculos Google explícitos. El API valida los ID tokens con Google y solo acepta audiencias
incluidas en `GOOGLE_CLIENT_IDS` (lista de Client IDs separada por comas). Una cuenta ya
registrada con el mismo correo debe iniciar sesión normalmente y vincular Google desde Ajustes.
Para vincularla, la persona debe estar autenticada en esa cuenta y confirmar la cuenta Google;
el vínculo no se realiza automáticamente por coincidencia de correo.

La sexta migración habilita solicitudes de recuperación con tokens aleatorios de un solo uso,
guardados como hash y con vencimiento de 30 minutos. Para enviar correos mediante la API HTTPS de
Resend configura `PUBLIC_API_URL`, `RESEND_API_KEY` y `EMAIL_FROM`. Verifica el dominio remitente
en Resend antes de usar una dirección propia. Los enlaces abren una página segura de la API para
elegir la nueva contraseña. La API limita las solicitudes por cuenta y responde con un mensaje
genérico para no revelar si un correo está registrado.

Para configurar Resend, crea una API key y verifica un dominio remitente en su panel. Agrega
los registros DNS que Resend indique en el proveedor de tu dominio y espera a que el dominio
aparezca verificado. En Render configura `RESEND_API_KEY` con la API key y `EMAIL_FROM` con una
dirección de ese dominio verificado, por ejemplo `Mis Finanzas <no-reply@tudominio.com>`.
`onboarding@resend.dev` solo sirve para pruebas limitadas y no debe usarse como remitente final.

El saldo inicial de efectivo, débito y ahorro representa dinero disponible; en una
cuenta de crédito representa deuda. Los gastos afectan la cuenta seleccionada, y un
retiro o pago de tarjeta se registra como transferencia para no duplicarlo como gasto.

## Render

- Build Command: `go build -o app ./cmd/api`
- Start Command: `./app`
- Health Check Path: `/health`
- Variables de entorno: `DATABASE_URL`, `JWT_SECRET`, `APP_TZ` (ej. `America/Mexico_City`),
  `GOOGLE_CLIENT_IDS` (opcional hasta habilitar Google), `CORS_ORIGINS` (orígenes web permitidos,
  separados por coma; agrega el dominio final del sitio web si se usa Google en web),
  `PUBLIC_API_URL` (URL HTTPS pública del servicio), `RESEND_API_KEY` (API key secreta de Resend)
  y `EMAIL_FROM` (dirección remitente verificada en Resend)
  para la recuperación por correo.

## Endpoints (prefijo `/api/v1`, todo con `Authorization: Bearer <token>` salvo auth)

| Método | Ruta | Descripción |
|---|---|---|
| POST | /auth/register, /auth/login | Registro / login (devuelven token) |
| POST | /auth/google | Valida token Google e inicia sesión o crea una cuenta |
| POST | /auth/google/link | Vincula Google a la cuenta autenticada |
| POST | /auth/password/forgot | Solicita un enlace de recuperación por correo |
| GET/POST | /auth/password/reset | Página web y aplicación de nueva contraseña |
| POST | /auth/password/change | Cambia la contraseña de la cuenta autenticada |
| GET/POST/DELETE | /categorias | Categorías (se crean por defecto al registrarse) |
| GET/POST/PUT/DELETE | /cuentas | Cuentas y saldos (DELETE archiva) |
| GET/POST | /transferencias | Retiros, traspasos y pagos de tarjeta |
| GET/POST | /gastos, PUT/DELETE /gastos/{id} | Gastos. Filtro: `?desde=&hasta=` (AAAA-MM-DD) |
| GET/POST | /ingresos, PUT/DELETE /ingresos/{id} | Ingresos / nómina |
| GET/POST | /recurrentes, PUT/DELETE /recurrentes/{id} | Servicios y gastos fijos mensuales |
| POST | /recurrentes/generar?mes=2026-10 | Crea los gastos del mes (idempotente) |
| GET/POST | /programados | Listar y crear gastos programados |
| PUT/DELETE | /programados/{id} | Editar o desactivar un gasto programado |
| GET/POST | /programados/{id}/apartados | Historial y registro de aportaciones virtuales |
| POST | /programados/{id}/pagar | Registra el gasto pagado y avanza el siguiente vencimiento |
| POST | /cuentas/{id}/conciliar | Ajusta el saldo calculado al saldo real sin crear ingreso/gasto |
| GET/POST/PUT/DELETE | /ingresos-programados | Consultar y gestionar ingresos esperados |
| POST | /ingresos-programados/{id}/recibir | Convierte un ingreso esperado en un ingreso real |
| GET/POST/DELETE | /ingresos-programados/{id}/asignaciones | Asignar el ingreso esperado a un gasto programado |
| GET | /flujo/quincena?ingreso_id=... | Flujo real y sobrante desde una nómina quincenal |
| GET | /resumen?mes=2026-10 | Ingresos, gastos, balance y gasto por categoría |
| GET | /sync/snapshot | Snapshot completo de datos financieros y borrados remotos |
| POST | /sync/push | Entrega de operaciones offline en lote, idempotente y con control de conflictos |

## Ejemplos

```bash
curl -X POST $URL/api/v1/auth/register -H 'Content-Type: application/json' \
  -d '{"nombre":"Ana","email":"ana@mail.com","password":"unaclave123"}'

curl -X POST $URL/api/v1/gastos -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"descripcion":"Gasolina","monto":850.50,"fecha":"2026-10-07"}'

curl -X POST $URL/api/v1/recurrentes -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"nombre":"Internet","monto":499,"dia_vencimiento":15}'
```

## Notas

- Render Free "duerme" el servicio: la primera petición tras inactividad tarda ~30 s.
- Los recurrentes se generan bajo demanda (`/recurrentes/generar`) en lugar de con un cron.
- Pendientes recomendados antes de publicar: límite de intentos de login, refresh tokens y pruebas.

## Sincronización offline (app nativa)

Ambas rutas requieren `Authorization: Bearer <token>`. El snapshot es una respuesta JSON con
arreglos (vacíos cuando no hay registros) para estas colecciones:

```json
{
  "categories": [],
  "accounts": [],
  "expenses": [],
  "incomes": [],
  "transfers": [],
  "recurring_services": [],
  "scheduled_expenses": [],
  "scheduled_expense_allocations": [],
  "scheduled_expense_payments": [],
  "scheduled_incomes": [],
  "scheduled_income_assignments": [],
  "balance_adjustments": [],
  "remote_deletions": []
}
```

Los nombres son estables y `snake_case`. Los objetos financieros usan los nombres de campos
actuales de la API (por ejemplo `nombre`, `monto`, `fecha`, `cuenta_id`) y omiten `usuario_id`
y `creado_en`. `accounts` también incluye `saldo_actual` calculado en el servidor.
`scheduled_expense_payments` es el subconjunto de `expenses` con `programado_id` no nulo;
el pago real permanece también en `expenses`. `remote_deletions` contiene
`{ "entity": "...", "entity_id": "<uuid>", "deleted_at": "<timestamp>" }`. Los borrados
físicos hechos por rutas existentes y por sync se registran en esta colección; reinsertar el
mismo UUID elimina su tombstone. Las rutas `DELETE` que archivan/desactivan (cuentas, servicios
recurrentes, gastos e ingresos programados) conservan el registro en su colección con
`activa: false`; no son tombstones.

`POST /api/v1/sync/push` acepta de 1 a 100 operaciones por petición (cuerpo máximo 1 MiB):

```json
{
  "operations": [{
    "op_id": "UUID estable de esta operación",
    "entity": "categories",
    "action": "create",
    "entity_id": "UUID estable del registro",
    "payload": { "nombre": "Casa", "tipo": "gasto" },
    "base_record": null
  }]
}
```

`entity` admite exactamente las colecciones snapshot excepto `remote_deletions`.
`action` admite `create`, `update` y `delete`; `payload` usa los campos mutables de la entidad
en el formato de la API. En create/update `entity_id` es el `id` que se almacenará; si
`payload.id` aparece, debe coincidir. En delete se requiere `payload: {}`. Update requiere
`base_record`: la versión completa del objeto tal como vino del snapshot, sin campos derivados
(`saldo_actual`), `usuario_id` ni `creado_en`. Si el registro cambió desde esa versión se
devuelve `conflict` sin sobrescribir los datos. Todas las referencias deben pertenecer al
usuario autenticado. `scheduled_expense_payments` acepta gastos programados vinculados a un
vencimiento (`programado_id` y `programado_fecha`); otros pagos programados aparecen como
gastos normales en `expenses` y el subconjunto también en `scheduled_expense_payments`.
Al crear un gasto vinculado, sync comprueba que sea el vencimiento vigente y avanza o desactiva
el gasto programado en la misma operación; la fecha del vencimiento no se puede cambiar al editar
el pago.

Campos admitidos en `payload` (todos los objetos snapshot incluyen además `id`; los campos con
valores por defecto pueden omitirse al crear cuando la restricción de la tabla lo permita):

| `entity` | Campos mutables |
|---|---|
| `categories` | `nombre`, `tipo` |
| `accounts` | `nombre`, `institucion`, `tipo`, `saldo_inicial`, `fecha_saldo_inicial`, `limite_credito`, `activa` |
| `expenses` | `categoria_id`, `recurrente_id`, `descripcion`, `monto`, `fecha`, `cuenta_id`, `programado_id`, `programado_fecha` |
| `incomes` | `categoria_id`, `descripcion`, `monto`, `fecha`, `periodicidad`, `cuenta_id`, `ingreso_programado_id`, `programado_fecha` |
| `transfers` | `cuenta_origen_id`, `cuenta_destino_id`, `descripcion`, `monto`, `fecha` |
| `recurring_services` | `categoria_id`, `cuenta_id`, `nombre`, `monto`, `dia_vencimiento`, `activo` |
| `scheduled_expenses` | `categoria_id`, `cuenta_id`, `nombre`, `monto`, `fecha_proximo_pago`, `dia_vencimiento`, `frecuencia`, `activo` |
| `scheduled_expense_allocations` | `programado_id`, `fecha_programada`, `monto`, `fecha`, `ingreso_programado_id`, `ingreso_fecha_programada` |
| `scheduled_expense_payments` | los campos de `expenses` |
| `scheduled_incomes` | `categoria_id`, `cuenta_id`, `nombre`, `monto`, `fecha_proximo_ingreso`, `dia_ingreso`, `frecuencia`, `certeza`, `activo` |
| `scheduled_income_assignments` | `ingreso_programado_id`, `fecha_ingreso_programada`, `gasto_programado_id`, `fecha_gasto_programada`, `monto` |
| `balance_adjustments` | `cuenta_id`, `monto`, `fecha`, `motivo` |

Los nombres de campo del payload conservan los nombres en español de las rutas existentes. Las
fechas son `YYYY-MM-DD`, las referencias UUID deben pertenecer al mismo usuario y los importes
usan los límites y validaciones ya definidos por la base de datos.

La respuesta es siempre `200` para el lote procesado; cada elemento informa su resultado:

```json
{
  "results": [{
    "op_id": "UUID de operación",
    "status": "applied",
    "entity": "categories",
    "entity_id": "UUID del registro"
  }]
}
```

Los estados posibles son `applied`, `duplicate`, `conflict` y `rejected`; `error` solo aparece
en resultados distintos de `applied`/`duplicate`. Reintentar el mismo `op_id` y contenido
después de aplicarlo devuelve `duplicate`. Cada operación del lote se confirma de manera
independiente y en orden: una operación inválida/conflictiva no revierte las demás. Para
refrescar conflictos o reconciliar borrados, vuelva a solicitar el snapshot. El snapshot es
completo (sin cursor ni paginación); los tombstones se conservan hasta que se restaura el mismo
UUID o se elimina la cuenta.

`results` confirma la operación y no incluye copias de registros derivados. Los movimientos
reales se sincronizan en sus colecciones (`expenses`/`scheduled_expense_payments` e `incomes`);
los ajustes de saldo son registros explícitos en `balance_adjustments`. Crear un `income` con
`ingreso_programado_id` comprueba el vencimiento vigente, convierte sus asignaciones vigentes
en apartados y avanza o desactiva el ingreso programado en la misma operación. Los apartados
resultantes se recuperan desde `scheduled_expense_allocations` en el snapshot; no se incluyen
en el objeto de resultado de push.
