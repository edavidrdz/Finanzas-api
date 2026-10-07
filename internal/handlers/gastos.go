package handlers

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"finanzas-api/internal/auth"
	"finanzas-api/internal/httpx"
)

type Gasto struct {
	ID           string  `json:"id"`
	CategoriaID  *string `json:"categoria_id"`
	Categoria    *string `json:"categoria,omitempty"`
	RecurrenteID *string `json:"recurrente_id,omitempty"`
	Descripcion  string  `json:"descripcion"`
	Monto        float64 `json:"monto"`
	Fecha        string  `json:"fecha"`
}

type gastoInput struct {
	CategoriaID *string `json:"categoria_id"`
	Descripcion string  `json:"descripcion"`
	Monto       float64 `json:"monto"`
	Fecha       string  `json:"fecha"` // AAAA-MM-DD; si va vacía, se usa hoy
}

func (a *API) ListGastos(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	desde, hasta, ok := a.rangoFechas(w, r)
	if !ok {
		return
	}
	rows, err := a.DB.Query(r.Context(), `
		SELECT g.id::text, g.categoria_id::text, c.nombre, g.recurrente_id::text,
		       g.descripcion, g.monto::float8, to_char(g.fecha, 'YYYY-MM-DD')
		FROM gastos g
		LEFT JOIN categorias c ON c.id = g.categoria_id
		WHERE g.usuario_id = $1 AND g.fecha BETWEEN $2 AND $3
		ORDER BY g.fecha DESC, g.creado_en DESC
		LIMIT 500`, uid, desde, hasta)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	defer rows.Close()
	out := []Gasto{}
	for rows.Next() {
		var g Gasto
		if err := rows.Scan(&g.ID, &g.CategoriaID, &g.Categoria, &g.RecurrenteID, &g.Descripcion, &g.Monto, &g.Fecha); err != nil {
			httpx.DBError(w, err)
			return
		}
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		httpx.DBError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (a *API) CreateGasto(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	var in gastoInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	fecha, msg := a.validarMovimiento(in.Descripcion, in.Monto, in.Fecha)
	if msg != "" {
		httpx.Error(w, http.StatusBadRequest, msg)
		return
	}
	in.Descripcion = strings.TrimSpace(in.Descripcion)
	in.CategoriaID = nilIfEmpty(in.CategoriaID)
	if ok, err := a.categoriaEsDelUsuario(r.Context(), uid, in.CategoriaID); err != nil {
		httpx.DBError(w, err)
		return
	} else if !ok {
		httpx.Error(w, http.StatusBadRequest, "categoría inválida")
		return
	}

	g := Gasto{CategoriaID: in.CategoriaID, Descripcion: in.Descripcion, Monto: in.Monto, Fecha: fecha.Format(dateLayout)}
	err := a.DB.QueryRow(r.Context(), `
		INSERT INTO gastos (usuario_id, categoria_id, descripcion, monto, fecha)
		VALUES ($1, $2, $3, $4::float8::numeric, $5) RETURNING id::text`,
		uid, in.CategoriaID, in.Descripcion, in.Monto, fecha).Scan(&g.ID)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, g)
}

func (a *API) UpdateGasto(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	id := chi.URLParam(r, "id")
	var in gastoInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	fecha, msg := a.validarMovimiento(in.Descripcion, in.Monto, in.Fecha)
	if msg != "" {
		httpx.Error(w, http.StatusBadRequest, msg)
		return
	}
	in.Descripcion = strings.TrimSpace(in.Descripcion)
	in.CategoriaID = nilIfEmpty(in.CategoriaID)
	if ok, err := a.categoriaEsDelUsuario(r.Context(), uid, in.CategoriaID); err != nil {
		httpx.DBError(w, err)
		return
	} else if !ok {
		httpx.Error(w, http.StatusBadRequest, "categoría inválida")
		return
	}
	tag, err := a.DB.Exec(r.Context(), `
		UPDATE gastos SET categoria_id = $3, descripcion = $4, monto = $5::float8::numeric, fecha = $6
		WHERE id = $1 AND usuario_id = $2`,
		id, uid, in.CategoriaID, in.Descripcion, in.Monto, fecha)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.Error(w, http.StatusNotFound, "no encontrado")
		return
	}
	httpx.JSON(w, http.StatusOK, Gasto{ID: id, CategoriaID: in.CategoriaID, Descripcion: in.Descripcion, Monto: in.Monto, Fecha: fecha.Format(dateLayout)})
}

func (a *API) DeleteGasto(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	tag, err := a.DB.Exec(r.Context(), `DELETE FROM gastos WHERE id = $1 AND usuario_id = $2`, chi.URLParam(r, "id"), uid)
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
