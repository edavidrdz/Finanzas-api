package handlers

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"finanzas-api/internal/auth"
	"finanzas-api/internal/httpx"
)

type Categoria struct {
	ID     string `json:"id"`
	Nombre string `json:"nombre"`
	Tipo   string `json:"tipo"`
}

func (a *API) ListCategorias(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	rows, err := a.DB.Query(r.Context(),
		`SELECT id::text, nombre, tipo FROM categorias WHERE usuario_id = $1 ORDER BY tipo, nombre`, uid)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	defer rows.Close()
	out := []Categoria{}
	for rows.Next() {
		var c Categoria
		if err := rows.Scan(&c.ID, &c.Nombre, &c.Tipo); err != nil {
			httpx.DBError(w, err)
			return
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		httpx.DBError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (a *API) CreateCategoria(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	var in struct {
		Nombre string `json:"nombre"`
		Tipo   string `json:"tipo"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	in.Nombre = strings.TrimSpace(in.Nombre)
	if in.Nombre == "" || len(in.Nombre) > 60 {
		httpx.Error(w, http.StatusBadRequest, "nombre es obligatorio (máx. 60 caracteres)")
		return
	}
	if in.Tipo != "gasto" && in.Tipo != "ingreso" {
		httpx.Error(w, http.StatusBadRequest, "tipo debe ser 'gasto' o 'ingreso'")
		return
	}
	c := Categoria{Nombre: in.Nombre, Tipo: in.Tipo}
	err := a.DB.QueryRow(r.Context(),
		`INSERT INTO categorias (usuario_id, nombre, tipo) VALUES ($1, $2, $3) RETURNING id::text`,
		uid, in.Nombre, in.Tipo).Scan(&c.ID)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, c)
}

func (a *API) DeleteCategoria(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	tag, err := a.DB.Exec(r.Context(),
		`DELETE FROM categorias WHERE id = $1 AND usuario_id = $2`, chi.URLParam(r, "id"), uid)
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
