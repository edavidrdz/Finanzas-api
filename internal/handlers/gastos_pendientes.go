package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"finanzas-api/internal/auth"
	"finanzas-api/internal/httpx"
)

type GastoPendiente struct {
	ID                string   `json:"id"`
	CategoriaID       *string  `json:"categoria_id"`
	CuentaID          *string  `json:"cuenta_id"`
	Descripcion       string   `json:"descripcion"`
	Monto             *float64 `json:"monto"`
	Fecha             string   `json:"fecha"`
	UbicacionLatitud  *float64 `json:"ubicacion_latitud"`
	UbicacionLongitud *float64 `json:"ubicacion_longitud"`
	UbicacionNombre   *string  `json:"ubicacion_nombre"`
	IniciadaEn        string   `json:"iniciada_en"`
}

type gastoPendienteInput struct {
	CategoriaID       *string  `json:"categoria_id"`
	CuentaID          *string  `json:"cuenta_id"`
	Descripcion       string   `json:"descripcion"`
	Monto             *float64 `json:"monto"`
	Fecha             string   `json:"fecha"`
	UbicacionLatitud  *float64 `json:"ubicacion_latitud"`
	UbicacionLongitud *float64 `json:"ubicacion_longitud"`
	UbicacionNombre   *string  `json:"ubicacion_nombre"`
	IniciadaEn        string   `json:"iniciada_en"`
}

func (a *API) ListGastosPendientes(w http.ResponseWriter, r *http.Request) {
	rows, err := a.DB.Query(r.Context(), `
		SELECT id::text, categoria_id::text, cuenta_id::text, descripcion, monto::float8,
		       to_char(fecha, 'YYYY-MM-DD'), ubicacion_latitud, ubicacion_longitud,
		       ubicacion_nombre, iniciada_en
		FROM gastos_pendientes
		WHERE usuario_id = $1
		ORDER BY iniciada_en DESC
		LIMIT 500`, auth.UserID(r.Context()))
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	defer rows.Close()
	out := []GastoPendiente{}
	for rows.Next() {
		var draft GastoPendiente
		var startedAt time.Time
		if err := rows.Scan(&draft.ID, &draft.CategoriaID, &draft.CuentaID, &draft.Descripcion,
			&draft.Monto, &draft.Fecha, &draft.UbicacionLatitud, &draft.UbicacionLongitud,
			&draft.UbicacionNombre, &startedAt); err != nil {
			httpx.DBError(w, err)
			return
		}
		draft.IniciadaEn = startedAt.Format(time.RFC3339Nano)
		out = append(out, draft)
	}
	if err := rows.Err(); err != nil {
		httpx.DBError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (a *API) CreateGastoPendiente(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	var in gastoPendienteInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	if msg := validarGastoPendiente(in); msg != "" {
		httpx.Error(w, http.StatusBadRequest, msg)
		return
	}
	in.Descripcion = strings.TrimSpace(in.Descripcion)
	in.CategoriaID = nilIfEmpty(in.CategoriaID)
	in.CuentaID = nilIfEmpty(in.CuentaID)
	if !a.validarReferenciasGastoPendiente(w, r, uid, in) {
		return
	}
	var draft GastoPendiente
	var startedAt time.Time
	err := a.DB.QueryRow(r.Context(), `
		INSERT INTO gastos_pendientes
			(usuario_id, categoria_id, cuenta_id, descripcion, monto, fecha, ubicacion_latitud,
			 ubicacion_longitud, ubicacion_nombre, iniciada_en)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id::text, categoria_id::text, cuenta_id::text, descripcion, monto::float8,
		          to_char(fecha, 'YYYY-MM-DD'), ubicacion_latitud, ubicacion_longitud,
		          ubicacion_nombre, iniciada_en`,
		uid, in.CategoriaID, in.CuentaID, in.Descripcion, in.Monto, in.Fecha,
		in.UbicacionLatitud, in.UbicacionLongitud, in.UbicacionNombre, in.IniciadaEn,
	).Scan(&draft.ID, &draft.CategoriaID, &draft.CuentaID, &draft.Descripcion, &draft.Monto,
		&draft.Fecha, &draft.UbicacionLatitud, &draft.UbicacionLongitud,
		&draft.UbicacionNombre, &startedAt)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	draft.IniciadaEn = startedAt.Format(time.RFC3339Nano)
	httpx.JSON(w, http.StatusCreated, draft)
}

func (a *API) UpdateGastoPendiente(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	var in gastoPendienteInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	if msg := validarGastoPendiente(in); msg != "" {
		httpx.Error(w, http.StatusBadRequest, msg)
		return
	}
	in.Descripcion = strings.TrimSpace(in.Descripcion)
	in.CategoriaID = nilIfEmpty(in.CategoriaID)
	in.CuentaID = nilIfEmpty(in.CuentaID)
	if !a.validarReferenciasGastoPendiente(w, r, uid, in) {
		return
	}
	var startedAt time.Time
	err := a.DB.QueryRow(r.Context(), `
		UPDATE gastos_pendientes
		SET categoria_id = $3, cuenta_id = $4, descripcion = $5, monto = $6, fecha = $7,
		    ubicacion_latitud = $8, ubicacion_longitud = $9, ubicacion_nombre = $10, iniciada_en = $11
		WHERE id = $1 AND usuario_id = $2
		RETURNING iniciada_en`,
		chi.URLParam(r, "id"), uid, in.CategoriaID, in.CuentaID, in.Descripcion,
		in.Monto, in.Fecha, in.UbicacionLatitud, in.UbicacionLongitud, in.UbicacionNombre,
		in.IniciadaEn).Scan(&startedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			httpx.Error(w, http.StatusNotFound, "no encontrado")
		} else {
			httpx.DBError(w, err)
		}
		return
	}
	draft := GastoPendiente{
		ID: chi.URLParam(r, "id"), CategoriaID: in.CategoriaID, CuentaID: in.CuentaID,
		Descripcion: in.Descripcion, Monto: in.Monto, Fecha: in.Fecha,
		UbicacionLatitud: in.UbicacionLatitud, UbicacionLongitud: in.UbicacionLongitud,
		UbicacionNombre: in.UbicacionNombre, IniciadaEn: startedAt.Format(time.RFC3339Nano),
	}
	httpx.JSON(w, http.StatusOK, draft)
}

func (a *API) DeleteGastoPendiente(w http.ResponseWriter, r *http.Request) {
	tag, err := a.DB.Exec(r.Context(),
		`DELETE FROM gastos_pendientes WHERE id = $1 AND usuario_id = $2`,
		chi.URLParam(r, "id"), auth.UserID(r.Context()))
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

func (a *API) CompleteGastoPendiente(w http.ResponseWriter, r *http.Request) {
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
	in.CategoriaID = nilIfEmpty(in.CategoriaID)
	in.CuentaID = nilIfEmpty(in.CuentaID)
	if !a.validarReferenciasGasto(w, r, uid, in) {
		return
	}
	tx, err := a.DB.Begin(r.Context())
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	defer tx.Rollback(r.Context())

	var gasto Gasto
	err = tx.QueryRow(r.Context(), `
		SELECT ubicacion_latitud, ubicacion_longitud, ubicacion_nombre
		FROM gastos_pendientes WHERE id = $1 AND usuario_id = $2 FOR UPDATE`,
		id, uid).Scan(&gasto.UbicacionLatitud, &gasto.UbicacionLongitud, &gasto.UbicacionNombre)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "el gasto pendiente ya no existe")
		return
	}
	gasto.ID, gasto.CategoriaID, gasto.CuentaID = id, in.CategoriaID, in.CuentaID
	gasto.Descripcion, gasto.Monto, gasto.Fecha = strings.TrimSpace(in.Descripcion), in.Monto, fecha.Format(dateLayout)
	_, err = tx.Exec(r.Context(), `
		INSERT INTO gastos
			(id, usuario_id, categoria_id, cuenta_id, descripcion, monto, fecha,
			 ubicacion_latitud, ubicacion_longitud, ubicacion_nombre)
		VALUES ($1, $2, $3, $4, $5, $6::float8::numeric, $7, $8, $9, $10)`,
		id, uid, gasto.CategoriaID, gasto.CuentaID, gasto.Descripcion, gasto.Monto, fecha,
		gasto.UbicacionLatitud, gasto.UbicacionLongitud, gasto.UbicacionNombre)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	if _, err := tx.Exec(r.Context(),
		`DELETE FROM gastos_pendientes WHERE id = $1 AND usuario_id = $2`, id, uid); err != nil {
		httpx.DBError(w, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpx.DBError(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, gasto)
}

func (a *API) validarReferenciasGastoPendiente(w http.ResponseWriter, r *http.Request, uid string, in gastoPendienteInput) bool {
	if ok, err := a.categoriaEsDelUsuario(r.Context(), uid, in.CategoriaID); err != nil {
		httpx.DBError(w, err)
		return false
	} else if !ok {
		httpx.Error(w, http.StatusBadRequest, "categoría inválida")
		return false
	}
	if ok, err := a.cuentaEsDelUsuario(r.Context(), uid, in.CuentaID); err != nil {
		httpx.DBError(w, err)
		return false
	} else if !ok {
		httpx.Error(w, http.StatusBadRequest, "cuenta inválida")
		return false
	}
	return true
}

func (a *API) validarReferenciasGasto(w http.ResponseWriter, r *http.Request, uid string, in gastoInput) bool {
	if ok, err := a.categoriaEsDelUsuario(r.Context(), uid, in.CategoriaID); err != nil {
		httpx.DBError(w, err)
		return false
	} else if !ok {
		httpx.Error(w, http.StatusBadRequest, "categoría inválida")
		return false
	}
	if ok, err := a.cuentaEsDelUsuario(r.Context(), uid, in.CuentaID); err != nil {
		httpx.DBError(w, err)
		return false
	} else if !ok {
		httpx.Error(w, http.StatusBadRequest, "cuenta inválida")
		return false
	}
	return true
}

func validarGastoPendiente(in gastoPendienteInput) string {
	if len(strings.TrimSpace(in.Descripcion)) > 200 {
		return "descripcion no puede exceder 200 caracteres"
	}
	if in.Monto != nil && (*in.Monto <= 0 || *in.Monto > 9999999999) {
		return "monto debe ser mayor a 0"
	}
	if _, err := time.Parse(dateLayout, in.Fecha); err != nil {
		return "fecha debe tener formato AAAA-MM-DD"
	}
	if _, err := time.Parse(time.RFC3339Nano, in.IniciadaEn); err != nil {
		return "iniciada_en debe tener formato RFC3339"
	}
	return validarUbicacion(in.UbicacionLatitud, in.UbicacionLongitud, in.UbicacionNombre)
}
