package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"finanzas-api/internal/auth"
	"finanzas-api/internal/httpx"
)

type Cuenta struct {
	ID                string   `json:"id"`
	Nombre            string   `json:"nombre"`
	Institucion       *string  `json:"institucion"`
	Tipo              string   `json:"tipo"`
	SaldoInicial      float64  `json:"saldo_inicial"`
	FechaSaldoInicial string   `json:"fecha_saldo_inicial"`
	LimiteCredito     *float64 `json:"limite_credito"`
	SaldoActual       float64  `json:"saldo_actual"`
	Activa            bool     `json:"activa"`
}

type cuentaInput struct {
	Nombre            string   `json:"nombre"`
	Institucion       *string  `json:"institucion"`
	Tipo              string   `json:"tipo"`
	SaldoInicial      float64  `json:"saldo_inicial"`
	FechaSaldoInicial string   `json:"fecha_saldo_inicial"`
	LimiteCredito     *float64 `json:"limite_credito"`
	Activa            *bool    `json:"activa"`
}

func cuentaTipoValido(tipo string) bool {
	switch tipo {
	case "efectivo", "debito", "credito", "ahorro":
		return true
	default:
		return false
	}
}

func (a *API) validarCuenta(in *cuentaInput) string {
	in.Nombre = strings.TrimSpace(in.Nombre)
	institucion := strings.TrimSpace(stringValue(in.Institucion))
	in.Institucion = nilIfEmpty(&institucion)
	if in.Nombre == "" || len(in.Nombre) > 100 {
		return "nombre es obligatorio (máx. 100 caracteres)"
	}
	if !cuentaTipoValido(in.Tipo) {
		return "tipo debe ser efectivo, debito, credito o ahorro"
	}
	if in.SaldoInicial < 0 || in.SaldoInicial > 9999999999 {
		return "saldo_inicial debe ser mayor o igual a 0"
	}
	if in.FechaSaldoInicial == "" {
		in.FechaSaldoInicial = a.hoy().Format(dateLayout)
	}
	if _, err := time.Parse(dateLayout, in.FechaSaldoInicial); err != nil {
		return "fecha_saldo_inicial debe tener formato AAAA-MM-DD"
	}
	if in.LimiteCredito != nil && (*in.LimiteCredito <= 0 || *in.LimiteCredito > 9999999999) {
		return "limite_credito debe ser mayor a 0"
	}
	if in.Tipo != "credito" && in.LimiteCredito != nil {
		return "limite_credito solo aplica a cuentas de crédito"
	}
	return ""
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

const cuentaBalancesSQL = `
	CASE WHEN c.tipo = 'credito' THEN
		c.saldo_inicial
		+ COALESCE((SELECT SUM(g.monto) FROM gastos g
			WHERE g.cuenta_id = c.id AND g.usuario_id = c.usuario_id AND g.fecha >= c.fecha_saldo_inicial), 0)
		- COALESCE((SELECT SUM(t.monto) FROM transferencias t
			WHERE t.cuenta_destino_id = c.id AND t.usuario_id = c.usuario_id AND t.fecha >= c.fecha_saldo_inicial), 0)
	ELSE
		c.saldo_inicial
		+ COALESCE((SELECT SUM(i.monto) FROM ingresos i
			WHERE i.cuenta_id = c.id AND i.usuario_id = c.usuario_id AND i.fecha >= c.fecha_saldo_inicial), 0)
		- COALESCE((SELECT SUM(g.monto) FROM gastos g
			WHERE g.cuenta_id = c.id AND g.usuario_id = c.usuario_id AND g.fecha >= c.fecha_saldo_inicial), 0)
		+ COALESCE((SELECT SUM(t.monto) FROM transferencias t
			WHERE t.cuenta_destino_id = c.id AND t.usuario_id = c.usuario_id AND t.fecha >= c.fecha_saldo_inicial), 0)
		- COALESCE((SELECT SUM(t.monto) FROM transferencias t
			WHERE t.cuenta_origen_id = c.id AND t.usuario_id = c.usuario_id AND t.fecha >= c.fecha_saldo_inicial), 0)
	END`

func (a *API) ListCuentas(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	rows, err := a.DB.Query(r.Context(), `
		SELECT c.id::text, c.nombre, c.institucion, c.tipo, c.saldo_inicial::float8,
		       to_char(c.fecha_saldo_inicial, 'YYYY-MM-DD'), c.limite_credito::float8,
		       (`+cuentaBalancesSQL+`)::float8, c.activa
		FROM cuentas c
		WHERE c.usuario_id = $1
		ORDER BY c.activa DESC, c.nombre`, uid)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	defer rows.Close()
	out := []Cuenta{}
	for rows.Next() {
		var cuenta Cuenta
		if err := rows.Scan(&cuenta.ID, &cuenta.Nombre, &cuenta.Institucion, &cuenta.Tipo,
			&cuenta.SaldoInicial, &cuenta.FechaSaldoInicial, &cuenta.LimiteCredito,
			&cuenta.SaldoActual, &cuenta.Activa); err != nil {
			httpx.DBError(w, err)
			return
		}
		out = append(out, cuenta)
	}
	if err := rows.Err(); err != nil {
		httpx.DBError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (a *API) CreateCuenta(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	var in cuentaInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	if msg := a.validarCuenta(&in); msg != "" {
		httpx.Error(w, http.StatusBadRequest, msg)
		return
	}
	cuenta := Cuenta{
		Nombre: in.Nombre, Institucion: in.Institucion, Tipo: in.Tipo,
		SaldoInicial: in.SaldoInicial, FechaSaldoInicial: in.FechaSaldoInicial,
		LimiteCredito: in.LimiteCredito, SaldoActual: in.SaldoInicial, Activa: true,
	}
	err := a.DB.QueryRow(r.Context(), `
		INSERT INTO cuentas (usuario_id, nombre, institucion, tipo, saldo_inicial,
			fecha_saldo_inicial, limite_credito)
		VALUES ($1, $2, $3, $4, $5::float8::numeric, $6, $7::float8::numeric)
		RETURNING id::text`,
		uid, in.Nombre, in.Institucion, in.Tipo, in.SaldoInicial, in.FechaSaldoInicial, in.LimiteCredito,
	).Scan(&cuenta.ID)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, cuenta)
}

func (a *API) UpdateCuenta(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	id := chi.URLParam(r, "id")
	var in cuentaInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	if msg := a.validarCuenta(&in); msg != "" {
		httpx.Error(w, http.StatusBadRequest, msg)
		return
	}
	activa := in.Activa == nil || *in.Activa
	tag, err := a.DB.Exec(r.Context(), `
		UPDATE cuentas SET nombre = $3, institucion = $4, tipo = $5,
			saldo_inicial = $6::float8::numeric, fecha_saldo_inicial = $7,
			limite_credito = $8::float8::numeric, activa = $9
		WHERE id = $1 AND usuario_id = $2`,
		id, uid, in.Nombre, in.Institucion, in.Tipo, in.SaldoInicial,
		in.FechaSaldoInicial, in.LimiteCredito, activa)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.Error(w, http.StatusNotFound, "no encontrado")
		return
	}
	var cuenta Cuenta
	err = a.DB.QueryRow(r.Context(), `
		SELECT c.id::text, c.nombre, c.institucion, c.tipo, c.saldo_inicial::float8,
		       to_char(c.fecha_saldo_inicial, 'YYYY-MM-DD'), c.limite_credito::float8,
		       (`+cuentaBalancesSQL+`)::float8, c.activa
		FROM cuentas c
		WHERE c.id = $1 AND c.usuario_id = $2`, id, uid).Scan(
		&cuenta.ID, &cuenta.Nombre, &cuenta.Institucion, &cuenta.Tipo,
		&cuenta.SaldoInicial, &cuenta.FechaSaldoInicial, &cuenta.LimiteCredito,
		&cuenta.SaldoActual, &cuenta.Activa)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, cuenta)
}

func (a *API) DeleteCuenta(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	tag, err := a.DB.Exec(r.Context(),
		`UPDATE cuentas SET activa = false WHERE id = $1 AND usuario_id = $2`,
		chi.URLParam(r, "id"), uid)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.Error(w, http.StatusNotFound, "no encontrado")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
