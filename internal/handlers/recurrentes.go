package handlers

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"finanzas-api/internal/auth"
	"finanzas-api/internal/httpx"
)

// Recurrente representa un gasto fijo mensual: luz, internet, gasolina, renta, etc.
type Recurrente struct {
	ID             string  `json:"id"`
	CategoriaID    *string `json:"categoria_id"`
	Categoria      *string `json:"categoria,omitempty"`
	Nombre         string  `json:"nombre"`
	Monto          float64 `json:"monto"`
	DiaVencimiento int     `json:"dia_vencimiento"`
	Activo         bool    `json:"activo"`
}

type recurrenteInput struct {
	CategoriaID    *string `json:"categoria_id"`
	Nombre         string  `json:"nombre"`
	Monto          float64 `json:"monto"`
	DiaVencimiento int     `json:"dia_vencimiento"`
	Activo         *bool   `json:"activo"` // por defecto true
}

// validar normaliza el input y devuelve un mensaje de error (vacío si todo está bien).
func (a *API) validarRecurrente(r *http.Request, uid string, in *recurrenteInput) string {
	in.Nombre = strings.TrimSpace(in.Nombre)
	in.CategoriaID = nilIfEmpty(in.CategoriaID)
	if in.Nombre == "" || len(in.Nombre) > 100 {
		return "nombre es obligatorio (máx. 100 caracteres)"
	}
	if in.Monto <= 0 || in.Monto > 9999999999 {
		return "monto debe ser mayor a 0"
	}
	if in.DiaVencimiento < 1 || in.DiaVencimiento > 31 {
		return "dia_vencimiento debe estar entre 1 y 31"
	}
	ok, err := a.categoriaEsDelUsuario(r.Context(), uid, in.CategoriaID)
	if err != nil || !ok {
		return "categoría inválida"
	}
	return ""
}

func (a *API) ListRecurrentes(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	rows, err := a.DB.Query(r.Context(), `
		SELECT r.id::text, r.categoria_id::text, c.nombre, r.nombre,
		       r.monto::float8, r.dia_vencimiento::int, r.activo
		FROM gastos_recurrentes r
		LEFT JOIN categorias c ON c.id = r.categoria_id
		WHERE r.usuario_id = $1
		ORDER BY r.dia_vencimiento, r.nombre`, uid)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	defer rows.Close()
	out := []Recurrente{}
	for rows.Next() {
		var x Recurrente
		if err := rows.Scan(&x.ID, &x.CategoriaID, &x.Categoria, &x.Nombre, &x.Monto, &x.DiaVencimiento, &x.Activo); err != nil {
			httpx.DBError(w, err)
			return
		}
		out = append(out, x)
	}
	if err := rows.Err(); err != nil {
		httpx.DBError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (a *API) CreateRecurrente(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	var in recurrenteInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	if msg := a.validarRecurrente(r, uid, &in); msg != "" {
		httpx.Error(w, http.StatusBadRequest, msg)
		return
	}
	activo := in.Activo == nil || *in.Activo
	x := Recurrente{CategoriaID: in.CategoriaID, Nombre: in.Nombre, Monto: in.Monto, DiaVencimiento: in.DiaVencimiento, Activo: activo}
	err := a.DB.QueryRow(r.Context(), `
		INSERT INTO gastos_recurrentes (usuario_id, categoria_id, nombre, monto, dia_vencimiento, activo)
		VALUES ($1, $2, $3, $4::float8::numeric, $5, $6) RETURNING id::text`,
		uid, in.CategoriaID, in.Nombre, in.Monto, in.DiaVencimiento, activo).Scan(&x.ID)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, x)
}

func (a *API) UpdateRecurrente(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	id := chi.URLParam(r, "id")
	var in recurrenteInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	if msg := a.validarRecurrente(r, uid, &in); msg != "" {
		httpx.Error(w, http.StatusBadRequest, msg)
		return
	}
	activo := in.Activo == nil || *in.Activo
	tag, err := a.DB.Exec(r.Context(), `
		UPDATE gastos_recurrentes
		SET categoria_id = $3, nombre = $4, monto = $5::float8::numeric, dia_vencimiento = $6, activo = $7
		WHERE id = $1 AND usuario_id = $2`,
		id, uid, in.CategoriaID, in.Nombre, in.Monto, in.DiaVencimiento, activo)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.Error(w, http.StatusNotFound, "no encontrado")
		return
	}
	httpx.JSON(w, http.StatusOK, Recurrente{ID: id, CategoriaID: in.CategoriaID, Nombre: in.Nombre, Monto: in.Monto, DiaVencimiento: in.DiaVencimiento, Activo: activo})
}

func (a *API) DeleteRecurrente(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	tag, err := a.DB.Exec(r.Context(), `DELETE FROM gastos_recurrentes WHERE id = $1 AND usuario_id = $2`, chi.URLParam(r, "id"), uid)
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

// GenerarRecurrentes crea los gastos del mes (?mes=AAAA-MM) a partir de los servicios activos.
// Es idempotente: si ya existen, no se duplican (índice único recurrente_id + fecha).
// Se llama desde la app (p. ej. al abrir un mes nuevo), así no dependemos de un cron en Render Free.
func (a *API) GenerarRecurrentes(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	primero, ok := a.primerDiaMes(w, r)
	if !ok {
		return
	}
	diasMes := primero.AddDate(0, 1, -1).Day()
	tag, err := a.DB.Exec(r.Context(), `
		INSERT INTO gastos (usuario_id, categoria_id, recurrente_id, descripcion, monto, fecha)
		SELECT r.usuario_id, r.categoria_id, r.id, r.nombre, r.monto,
		       $2::date + (LEAST(r.dia_vencimiento::int, $3::int) - 1)
		FROM gastos_recurrentes r
		WHERE r.usuario_id = $1 AND r.activo
		ON CONFLICT (recurrente_id, fecha) WHERE recurrente_id IS NOT NULL DO NOTHING`,
		uid, primero, diasMes)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"mes":     primero.Format("2006-01"),
		"creados": tag.RowsAffected(),
	})
}
