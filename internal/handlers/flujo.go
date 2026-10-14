package handlers

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"finanzas-api/internal/auth"
	"finanzas-api/internal/httpx"
)

type FlujoQuincena struct {
	IngresoNominaID            string  `json:"ingreso_nomina_id"`
	Descripcion                string  `json:"descripcion"`
	FechaNomina                string  `json:"fecha_nomina"`
	FechaInicio                string  `json:"fecha_inicio"`
	FechaFin                   string  `json:"fecha_fin"`
	Nomina                     float64 `json:"nomina"`
	SaldoDisponibleInicio      float64 `json:"saldo_disponible_inicio"`
	OtrosIngresos              float64 `json:"otros_ingresos"`
	GastosEfectivo             float64 `json:"gastos_efectivo"`
	GastosTarjeta              float64 `json:"gastos_tarjeta"`
	PagosTarjeta               float64 `json:"pagos_tarjeta"`
	Ajustes                    float64 `json:"ajustes"`
	ApartadosPeriodo           float64 `json:"apartados_periodo"`
	SaldoDisponible            float64 `json:"saldo_disponible"`
	ReservadoActual            float64 `json:"reservado_actual"`
	DisponibleDespuesApartados float64 `json:"disponible_despues_apartados"`
	EsUltimaNomina             bool    `json:"es_ultima_nomina"`
}

// cuentaBalancesAlDiaSQL calcula el saldo reconstruido de una cuenta hasta $2.
const cuentaBalancesAlDiaSQL = `
	CASE WHEN c.tipo = 'credito' THEN
		c.saldo_inicial
		+ COALESCE((SELECT SUM(g.monto) FROM gastos g
			WHERE g.cuenta_id = c.id AND g.usuario_id = c.usuario_id
			  AND g.fecha >= c.fecha_saldo_inicial AND g.fecha <= $2), 0)
		- COALESCE((SELECT SUM(t.monto) FROM transferencias t
			WHERE t.cuenta_destino_id = c.id AND t.usuario_id = c.usuario_id
			  AND t.fecha >= c.fecha_saldo_inicial AND t.fecha <= $2), 0)
		+ COALESCE((SELECT SUM(a.monto) FROM ajustes_saldo a
			WHERE a.cuenta_id = c.id AND a.usuario_id = c.usuario_id
			  AND a.fecha >= c.fecha_saldo_inicial AND a.fecha <= $2), 0)
	ELSE
		c.saldo_inicial
		+ COALESCE((SELECT SUM(i.monto) FROM ingresos i
			WHERE i.cuenta_id = c.id AND i.usuario_id = c.usuario_id
			  AND i.fecha >= c.fecha_saldo_inicial AND i.fecha <= $2), 0)
		- COALESCE((SELECT SUM(g.monto) FROM gastos g
			WHERE g.cuenta_id = c.id AND g.usuario_id = c.usuario_id
			  AND g.fecha >= c.fecha_saldo_inicial AND g.fecha <= $2), 0)
		+ COALESCE((SELECT SUM(t.monto) FROM transferencias t
			WHERE t.cuenta_destino_id = c.id AND t.usuario_id = c.usuario_id
			  AND t.fecha >= c.fecha_saldo_inicial AND t.fecha <= $2), 0)
		- COALESCE((SELECT SUM(t.monto) FROM transferencias t
			WHERE t.cuenta_origen_id = c.id AND t.usuario_id = c.usuario_id
			  AND t.fecha >= c.fecha_saldo_inicial AND t.fecha <= $2), 0)
		+ COALESCE((SELECT SUM(a.monto) FROM ajustes_saldo a
			WHERE a.cuenta_id = c.id AND a.usuario_id = c.usuario_id
			  AND a.fecha >= c.fecha_saldo_inicial AND a.fecha <= $2), 0)
	END`

func (a *API) GetFlujoQuincena(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	fechaHoy := a.hoy()
	ingresoID := r.URL.Query().Get("ingreso_id")
	var flujo FlujoQuincena
	var fechaSiguiente *time.Time
	var err error
	if ingresoID == "" {
		err = a.DB.QueryRow(r.Context(), `
			SELECT i.id::text, i.descripcion, i.fecha, i.monto::float8
			FROM ingresos i
			WHERE i.usuario_id = $1 AND i.periodicidad = 'quincenal' AND i.fecha <= $2
			ORDER BY i.fecha DESC, i.creado_en DESC
			LIMIT 1`, uid, fechaHoy).Scan(&flujo.IngresoNominaID, &flujo.Descripcion, &fechaHoy, &flujo.Nomina)
	} else {
		err = a.DB.QueryRow(r.Context(), `
			SELECT i.id::text, i.descripcion, i.fecha, i.monto::float8
			FROM ingresos i
			WHERE i.id = $1 AND i.usuario_id = $2 AND i.periodicidad = 'quincenal'`,
			ingresoID, uid).Scan(&flujo.IngresoNominaID, &flujo.Descripcion, &fechaHoy, &flujo.Nomina)
	}
	if err == nil && fechaHoy.After(a.hoy()) {
		httpx.Error(w, http.StatusBadRequest, "la nómina seleccionada tiene una fecha futura")
		return
	}
	if err != nil {
		if err == pgx.ErrNoRows {
			httpx.Error(w, http.StatusNotFound, "registra un ingreso de nómina con periodicidad quincenal para consultar el flujo")
			return
		}
		httpx.DBError(w, err)
		return
	}
	fechaInicio := fechaHoy
	flujo.FechaNomina = fechaInicio.Format(dateLayout)
	flujo.FechaInicio = flujo.FechaNomina
	err = a.DB.QueryRow(r.Context(), `
		SELECT MIN(fecha) FROM ingresos
		WHERE usuario_id = $1 AND periodicidad = 'quincenal' AND fecha > $2`,
		uid, fechaInicio).Scan(&fechaSiguiente)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	hasta := fechaHoy
	flujo.EsUltimaNomina = fechaSiguiente == nil
	if fechaSiguiente != nil {
		hasta = fechaSiguiente.AddDate(0, 0, -1)
	}
	if hasta.After(a.hoy()) {
		hasta = a.hoy()
	}
	if hasta.Before(fechaInicio) {
		hasta = fechaInicio
	}
	flujo.FechaFin = hasta.Format(dateLayout)

	if err := a.DB.QueryRow(r.Context(), `
		SELECT COALESCE(SUM(CASE
			WHEN c.fecha_saldo_inicial > $2 THEN 0
			ELSE (`+cuentaBalancesAlDiaSQL+`)::float8
		END), 0)
		FROM cuentas c
		WHERE c.usuario_id = $1 AND c.activa AND c.tipo <> 'credito'`,
		uid, fechaInicio.AddDate(0, 0, -1)).Scan(&flujo.SaldoDisponibleInicio); err != nil {
		httpx.DBError(w, err)
		return
	}

	err = a.DB.QueryRow(r.Context(), `
		SELECT
			COALESCE(SUM(i.monto) FILTER (WHERE i.id <> $2), 0)::float8
		FROM ingresos i
		LEFT JOIN cuentas c ON c.id = i.cuenta_id AND c.usuario_id = i.usuario_id
		WHERE i.usuario_id = $1 AND i.fecha BETWEEN $3 AND $4
		  AND (i.cuenta_id IS NULL OR c.tipo <> 'credito')`,
		uid, flujo.IngresoNominaID, fechaInicio, hasta).Scan(&flujo.OtrosIngresos)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	err = a.DB.QueryRow(r.Context(), `
		SELECT
			COALESCE(SUM(g.monto) FILTER (WHERE g.cuenta_id IS NULL OR c.tipo <> 'credito'), 0)::float8,
			COALESCE(SUM(g.monto) FILTER (WHERE c.tipo = 'credito'), 0)::float8
		FROM gastos g
		LEFT JOIN cuentas c ON c.id = g.cuenta_id AND c.usuario_id = g.usuario_id
		WHERE g.usuario_id = $1 AND g.fecha BETWEEN $2 AND $3`,
		uid, fechaInicio, hasta).Scan(&flujo.GastosEfectivo, &flujo.GastosTarjeta)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	err = a.DB.QueryRow(r.Context(), `
		SELECT COALESCE(SUM(t.monto), 0)::float8
		FROM transferencias t
		JOIN cuentas origen ON origen.id = t.cuenta_origen_id AND origen.usuario_id = t.usuario_id
		JOIN cuentas destino ON destino.id = t.cuenta_destino_id AND destino.usuario_id = t.usuario_id
		WHERE t.usuario_id = $1 AND t.fecha BETWEEN $2 AND $3
		  AND origen.tipo <> 'credito' AND destino.tipo = 'credito'`,
		uid, fechaInicio, hasta).Scan(&flujo.PagosTarjeta)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	err = a.DB.QueryRow(r.Context(), `
		SELECT COALESCE(SUM(a.monto), 0)::float8
		FROM ajustes_saldo a
		JOIN cuentas c ON c.id = a.cuenta_id AND c.usuario_id = a.usuario_id
		WHERE a.usuario_id = $1 AND a.fecha BETWEEN $2 AND $3 AND c.tipo <> 'credito'`,
		uid, fechaInicio, hasta).Scan(&flujo.Ajustes)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	err = a.DB.QueryRow(r.Context(), `
		SELECT COALESCE(SUM(monto), 0)::float8
		FROM programado_apartados
		WHERE usuario_id = $1 AND fecha BETWEEN $2 AND $3`,
		uid, fechaInicio, hasta).Scan(&flujo.ApartadosPeriodo)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	err = a.DB.QueryRow(r.Context(), `
		SELECT COALESCE(SUM((
			SELECT COALESCE(SUM(ap.monto), 0)
			FROM programado_apartados ap
			WHERE ap.programado_id = p.id AND ap.fecha_programada = p.fecha_proximo_pago
		)), 0)::float8
		FROM gastos_programados p
		WHERE p.usuario_id = $1 AND p.activo`, uid).Scan(&flujo.ReservadoActual)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	flujo.SaldoDisponible = calcularDisponibleQuincena(
		flujo.SaldoDisponibleInicio, flujo.Nomina, flujo.OtrosIngresos, flujo.Ajustes,
		flujo.GastosEfectivo, flujo.PagosTarjeta,
	)
	flujo.DisponibleDespuesApartados = flujo.SaldoDisponible - flujo.ReservadoActual
	httpx.JSON(w, http.StatusOK, flujo)
}

func calcularDisponibleQuincena(saldoInicio, nomina, otrosIngresos, ajustes, gastosEfectivo, pagosTarjeta float64) float64 {
	return saldoInicio + nomina + otrosIngresos + ajustes - gastosEfectivo - pagosTarjeta
}
