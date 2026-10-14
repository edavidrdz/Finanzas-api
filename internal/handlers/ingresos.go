package handlers

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"finanzas-api/internal/auth"
	"finanzas-api/internal/httpx"
)

type Ingreso struct {
	ID           string  `json:"id"`
	CategoriaID  *string `json:"categoria_id"`
	Categoria    *string `json:"categoria,omitempty"`
	CuentaID     *string `json:"cuenta_id"`
	Cuenta       *string `json:"cuenta,omitempty"`
	Descripcion  string  `json:"descripcion"`
	Monto        float64 `json:"monto"`
	Fecha        string  `json:"fecha"`
	Periodicidad string  `json:"periodicidad"`
}

type ingresoInput struct {
	CategoriaID  *string `json:"categoria_id"`
	CuentaID     *string `json:"cuenta_id"`
	Descripcion  string  `json:"descripcion"`
	Monto        float64 `json:"monto"`
	Fecha        string  `json:"fecha"`
	Periodicidad string  `json:"periodicidad"` // unico | semanal | quincenal | mensual
}

func periodicidadValida(p string) bool {
	switch p {
	case "unico", "semanal", "quincenal", "mensual":
		return true
	}
	return false
}

func (a *API) ListIngresos(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	desde, hasta, ok := a.rangoFechas(w, r)
	if !ok {
		return
	}
	rows, err := a.DB.Query(r.Context(), `
		SELECT i.id::text, i.categoria_id::text, c.nombre, i.descripcion,
		       i.cuenta_id::text, cta.nombre, i.monto::float8, to_char(i.fecha, 'YYYY-MM-DD'), i.periodicidad
		FROM ingresos i
		LEFT JOIN categorias c ON c.id = i.categoria_id
		LEFT JOIN cuentas cta ON cta.id = i.cuenta_id
		WHERE i.usuario_id = $1 AND i.fecha BETWEEN $2 AND $3
		ORDER BY i.fecha DESC, i.creado_en DESC
		LIMIT 500`, uid, desde, hasta)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	defer rows.Close()
	out := []Ingreso{}
	for rows.Next() {
		var i Ingreso
		if err := rows.Scan(&i.ID, &i.CategoriaID, &i.Categoria, &i.Descripcion,
			&i.CuentaID, &i.Cuenta, &i.Monto, &i.Fecha, &i.Periodicidad); err != nil {
			httpx.DBError(w, err)
			return
		}
		out = append(out, i)
	}
	if err := rows.Err(); err != nil {
		httpx.DBError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (a *API) CreateIngreso(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	var in ingresoInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	if in.Periodicidad == "" {
		in.Periodicidad = "unico"
	}
	if !periodicidadValida(in.Periodicidad) {
		httpx.Error(w, http.StatusBadRequest, "periodicidad debe ser unico, semanal, quincenal o mensual")
		return
	}
	fecha, msg := a.validarMovimiento(in.Descripcion, in.Monto, in.Fecha)
	if msg != "" {
		httpx.Error(w, http.StatusBadRequest, msg)
		return
	}
	in.Descripcion = strings.TrimSpace(in.Descripcion)
	in.CategoriaID = nilIfEmpty(in.CategoriaID)
	in.CuentaID = nilIfEmpty(in.CuentaID)
	if ok, err := a.categoriaEsDelUsuario(r.Context(), uid, in.CategoriaID); err != nil {
		httpx.DBError(w, err)
		return
	} else if !ok {
		httpx.Error(w, http.StatusBadRequest, "categoría inválida")
		return
	}
	if ok, err := a.cuentaAceptaIngreso(r.Context(), uid, in.CuentaID); err != nil {
		httpx.DBError(w, err)
		return
	} else if !ok {
		httpx.Error(w, http.StatusBadRequest, "cuenta inválida para recibir ingresos")
		return
	}

	i := Ingreso{CategoriaID: in.CategoriaID, CuentaID: in.CuentaID, Descripcion: in.Descripcion, Monto: in.Monto, Fecha: fecha.Format(dateLayout), Periodicidad: in.Periodicidad}
	err := a.DB.QueryRow(r.Context(), `
		INSERT INTO ingresos (usuario_id, categoria_id, cuenta_id, descripcion, monto, fecha, periodicidad)
		VALUES ($1, $2, $3, $4, $5::float8::numeric, $6, $7) RETURNING id::text`,
		uid, in.CategoriaID, in.CuentaID, in.Descripcion, in.Monto, fecha, in.Periodicidad).Scan(&i.ID)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, i)
}

func (a *API) UpdateIngreso(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	id := chi.URLParam(r, "id")
	var in ingresoInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	if in.Periodicidad == "" {
		in.Periodicidad = "unico"
	}
	if !periodicidadValida(in.Periodicidad) {
		httpx.Error(w, http.StatusBadRequest, "periodicidad debe ser unico, semanal, quincenal o mensual")
		return
	}
	fecha, msg := a.validarMovimiento(in.Descripcion, in.Monto, in.Fecha)
	if msg != "" {
		httpx.Error(w, http.StatusBadRequest, msg)
		return
	}
	in.Descripcion = strings.TrimSpace(in.Descripcion)
	in.CategoriaID = nilIfEmpty(in.CategoriaID)
	in.CuentaID = nilIfEmpty(in.CuentaID)
	if ok, err := a.categoriaEsDelUsuario(r.Context(), uid, in.CategoriaID); err != nil {
		httpx.DBError(w, err)
		return
	} else if !ok {
		httpx.Error(w, http.StatusBadRequest, "categoría inválida")
		return
	}
	if ok, err := a.cuentaAceptaIngreso(r.Context(), uid, in.CuentaID); err != nil {
		httpx.DBError(w, err)
		return
	} else if !ok {
		httpx.Error(w, http.StatusBadRequest, "cuenta inválida para recibir ingresos")
		return
	}
	tag, err := a.DB.Exec(r.Context(), `
		UPDATE ingresos SET categoria_id = $3, cuenta_id = $4, descripcion = $5, monto = $6::float8::numeric, fecha = $7, periodicidad = $8
		WHERE id = $1 AND usuario_id = $2`,
		id, uid, in.CategoriaID, in.CuentaID, in.Descripcion, in.Monto, fecha, in.Periodicidad)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.Error(w, http.StatusNotFound, "no encontrado")
		return
	}
	httpx.JSON(w, http.StatusOK, Ingreso{ID: id, CategoriaID: in.CategoriaID, CuentaID: in.CuentaID, Descripcion: in.Descripcion, Monto: in.Monto, Fecha: fecha.Format(dateLayout), Periodicidad: in.Periodicidad})
}

func (a *API) DeleteIngreso(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	tag, err := a.DB.Exec(r.Context(), `DELETE FROM ingresos WHERE id = $1 AND usuario_id = $2`, chi.URLParam(r, "id"), uid)
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
