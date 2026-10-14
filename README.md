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
cp .env.example .env.example        # edita DATABASE_URL y JWT_SECRET
export $(grep -v '^#' .env.example | xargs)
go run ./cmd/api
```

Verifica: `curl localhost:8080/health/db`

Al actualizar una instalación existente, ejecuta en el SQL Editor de Neon, en orden:

1. `migrations/001_init.sql` (solo necesario si aún no se creó el esquema base).
2. `migrations/002_cuentas_y_transferencias.sql`.

La segunda migración agrega cuentas financieras y transferencias sin borrar los gastos
ni ingresos existentes. Los movimientos anteriores quedan sin cuenta asignada y no se
incluyen en el saldo calculado desde la fecha de saldo inicial de cada cuenta.

El saldo inicial de efectivo, débito y ahorro representa dinero disponible; en una
cuenta de crédito representa deuda. Los gastos afectan la cuenta seleccionada, y un
retiro o pago de tarjeta se registra como transferencia para no duplicarlo como gasto.

## Render

- Build Command: `go build -o app ./cmd/api`
- Start Command: `./app`
- Health Check Path: `/health`
- Variables de entorno: `DATABASE_URL`, `JWT_SECRET`, `APP_TZ` (ej. `America/Mexico_City`)

## Endpoints (prefijo `/api/v1`, todo con `Authorization: Bearer <token>` salvo auth)

| Método | Ruta | Descripción |
|---|---|---|
| POST | /auth/register, /auth/login | Registro / login (devuelven token) |
| GET/POST/DELETE | /categorias | Categorías (se crean por defecto al registrarse) |
| GET/POST/PUT/DELETE | /cuentas | Cuentas y saldos (DELETE archiva) |
| GET/POST | /transferencias | Retiros, traspasos y pagos de tarjeta |
| GET/POST | /gastos, PUT/DELETE /gastos/{id} | Gastos. Filtro: `?desde=&hasta=` (AAAA-MM-DD) |
| GET/POST | /ingresos, PUT/DELETE /ingresos/{id} | Ingresos / nómina |
| GET/POST | /recurrentes, PUT/DELETE /recurrentes/{id} | Servicios y gastos fijos mensuales |
| POST | /recurrentes/generar?mes=2026-10 | Crea los gastos del mes (idempotente) |
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
