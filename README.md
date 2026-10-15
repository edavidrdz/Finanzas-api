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
