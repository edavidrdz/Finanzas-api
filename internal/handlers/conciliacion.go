package handlers

import (
	"math"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"finanzas-api/internal/auth"
	"finanzas-api/internal/httpx"
)

type ajusteSaldoInput struct {
	SaldoReal float64 `json:"saldo_real"`
	Motivo    string  `json:"motivo"`
}

type AjusteSaldo struct {
	ID            string  `json:"id"`
	CuentaID      string  `json:"cuenta_id"`
	SaldoAnterior float64 `json:"saldo_anterior"`
	SaldoReal     float64 `json:"saldo_real"`
	Ajuste        float64 `json:"ajuste"`
	Fecha         string  `json:"fecha"`
	Motivo        string  `json:"motivo"`
}

func (a *API) ConciliarCuenta(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	cuentaID := chi.URLParam(r, "id")
	var in ajusteSaldoInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	if in.SaldoReal < 0 || in.SaldoReal > 9999999999 {
		httpx.Error(w, http.StatusBadRequest, "saldo_real debe ser mayor o igual a 0")
		return
	}
	fecha := a.hoy()
	motivo := strings.TrimSpace(in.Motivo)
	if motivo == "" {
		motivo = "Conciliación de saldo"
	}
	if len(motivo) > 200 {
		httpx.Error(w, http.StatusBadRequest, "motivo no puede superar 200 caracteres")
		return
	}

	tx, err := a.DB.Begin(r.Context())
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	defer tx.Rollback(r.Context())

	var lockedID string
	err = tx.QueryRow(r.Context(), `
		SELECT id::text FROM cuentas
		WHERE id = $1 AND usuario_id = $2 AND activa
		FOR UPDATE`, cuentaID, uid).Scan(&lockedID)
	if err == pgx.ErrNoRows {
		httpx.Error(w, http.StatusNotFound, "cuenta no encontrada o inactiva")
		return
	}
	if err != nil {
		httpx.DBError(w, err)
		return
	}

	var actual float64
	err = tx.QueryRow(r.Context(), `
		SELECT (`+cuentaBalancesSQL+`)::float8
		FROM cuentas c WHERE c.id = $1 AND c.usuario_id = $2`,
		cuentaID, uid).Scan(&actual)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	ajuste := math.Round((in.SaldoReal-actual)*100) / 100
	out := AjusteSaldo{
		CuentaID: cuentaID, SaldoAnterior: actual, SaldoReal: in.SaldoReal,
		Ajuste: ajuste, Fecha: fecha.Format(dateLayout), Motivo: motivo,
	}
	if ajuste == 0 {
		httpx.JSON(w, http.StatusOK, out)
		return
	}
	err = tx.QueryRow(r.Context(), `
		INSERT INTO ajustes_saldo (usuario_id, cuenta_id, monto, fecha, motivo)
		VALUES ($1, $2, $3::float8::numeric, $4, $5)
		RETURNING id::text`, uid, cuentaID, ajuste, fecha, motivo).Scan(&out.ID)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpx.DBError(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, out)
}
