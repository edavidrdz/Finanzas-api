package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"finanzas-api/internal/auth"
	"finanzas-api/internal/httpx"
)

type Transferencia struct {
	ID              string  `json:"id"`
	CuentaOrigenID  string  `json:"cuenta_origen_id"`
	CuentaOrigen    string  `json:"cuenta_origen"`
	CuentaDestinoID string  `json:"cuenta_destino_id"`
	CuentaDestino   string  `json:"cuenta_destino"`
	Descripcion     string  `json:"descripcion"`
	Monto           float64 `json:"monto"`
	Fecha           string  `json:"fecha"`
}

type transferenciaInput struct {
	CuentaOrigenID  string  `json:"cuenta_origen_id"`
	CuentaDestinoID string  `json:"cuenta_destino_id"`
	Descripcion     string  `json:"descripcion"`
	Monto           float64 `json:"monto"`
	Fecha           string  `json:"fecha"`
}

func (a *API) ListTransferencias(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	desde, hasta, ok := a.rangoFechas(w, r)
	if !ok {
		return
	}
	rows, err := a.DB.Query(r.Context(), `
		SELECT t.id::text, t.cuenta_origen_id::text, origen.nombre,
		       t.cuenta_destino_id::text, destino.nombre, t.descripcion, t.monto::float8,
		       to_char(t.fecha, 'YYYY-MM-DD')
		FROM transferencias t
		JOIN cuentas origen ON origen.id = t.cuenta_origen_id
		JOIN cuentas destino ON destino.id = t.cuenta_destino_id
		WHERE t.usuario_id = $1 AND t.fecha BETWEEN $2 AND $3
		ORDER BY t.fecha DESC, t.creado_en DESC
		LIMIT 500`, uid, desde, hasta)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	defer rows.Close()
	out := []Transferencia{}
	for rows.Next() {
		var t Transferencia
		if err := rows.Scan(&t.ID, &t.CuentaOrigenID, &t.CuentaOrigen,
			&t.CuentaDestinoID, &t.CuentaDestino, &t.Descripcion, &t.Monto, &t.Fecha); err != nil {
			httpx.DBError(w, err)
			return
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		httpx.DBError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (a *API) CreateTransferencia(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	var in transferenciaInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	in.CuentaOrigenID = strings.TrimSpace(in.CuentaOrigenID)
	in.CuentaDestinoID = strings.TrimSpace(in.CuentaDestinoID)
	in.Descripcion = strings.TrimSpace(in.Descripcion)
	if in.CuentaOrigenID == "" || in.CuentaDestinoID == "" || in.CuentaOrigenID == in.CuentaDestinoID {
		httpx.Error(w, http.StatusBadRequest, "selecciona dos cuentas diferentes")
		return
	}
	if in.Descripcion == "" {
		in.Descripcion = "Transferencia"
	}
	if len(in.Descripcion) > 200 {
		httpx.Error(w, http.StatusBadRequest, "descripcion no puede superar 200 caracteres")
		return
	}
	if in.Monto <= 0 || in.Monto > 9999999999 {
		httpx.Error(w, http.StatusBadRequest, "monto debe ser mayor a 0")
		return
	}
	fecha, err := time.Parse(dateLayout, in.Fecha)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "fecha debe tener formato AAAA-MM-DD")
		return
	}

	var tipoOrigen, nombreOrigen, nombreDestino string
	err = a.DB.QueryRow(r.Context(), `
		SELECT origen.tipo, origen.nombre, destino.nombre
		FROM cuentas origen
		JOIN cuentas destino ON destino.id = $3 AND destino.usuario_id = $1 AND destino.activa
		WHERE origen.id = $2 AND origen.usuario_id = $1 AND origen.activa`,
		uid, in.CuentaOrigenID, in.CuentaDestinoID,
	).Scan(&tipoOrigen, &nombreOrigen, &nombreDestino)
	if err != nil {
		if err == pgx.ErrNoRows {
			httpx.Error(w, http.StatusBadRequest, "cuenta inválida o inactiva")
			return
		}
		httpx.DBError(w, err)
		return
	}
	if tipoOrigen == "credito" {
		httpx.Error(w, http.StatusBadRequest, "no se permiten transferencias desde una tarjeta de crédito")
		return
	}

	out := Transferencia{
		CuentaOrigenID: in.CuentaOrigenID, CuentaOrigen: nombreOrigen,
		CuentaDestinoID: in.CuentaDestinoID, CuentaDestino: nombreDestino,
		Descripcion: in.Descripcion, Monto: in.Monto, Fecha: fecha.Format(dateLayout),
	}
	err = a.DB.QueryRow(r.Context(), `
		INSERT INTO transferencias
			(usuario_id, cuenta_origen_id, cuenta_destino_id, descripcion, monto, fecha)
		VALUES ($1, $2, $3, $4, $5::float8::numeric, $6)
		RETURNING id::text`,
		uid, in.CuentaOrigenID, in.CuentaDestinoID, in.Descripcion, in.Monto, fecha,
	).Scan(&out.ID)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, out)
}
