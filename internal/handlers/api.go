package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"finanzas-api/internal/httpx"
)

const dateLayout = "2006-01-02"

type API struct {
	DB        *pgxpool.Pool
	JWTSecret string
	Loc       *time.Location // zona horaria del usuario (APP_TZ)
}

func (a *API) HealthDB(w http.ResponseWriter, r *http.Request) {
	if err := a.DB.Ping(r.Context()); err != nil {
		httpx.Error(w, http.StatusServiceUnavailable, "base de datos no disponible")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// hoy devuelve la fecha actual (sin hora) en la zona horaria configurada.
func (a *API) hoy() time.Time {
	n := time.Now().In(a.Loc)
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
}

// rangoMes devuelve el primer día del mes indicado en ?mes=AAAA-MM (por defecto, el mes actual).
func (a *API) primerDiaMes(w http.ResponseWriter, r *http.Request) (time.Time, bool) {
	s := r.URL.Query().Get("mes")
	if s == "" {
		h := a.hoy()
		return time.Date(h.Year(), h.Month(), 1, 0, 0, 0, 0, time.UTC), true
	}
	t, err := time.Parse("2006-01", s)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "mes debe tener formato AAAA-MM")
		return time.Time{}, false
	}
	return t, true
}

// rangoFechas lee ?desde=&hasta= (AAAA-MM-DD). Por defecto: el mes actual.
func (a *API) rangoFechas(w http.ResponseWriter, r *http.Request) (time.Time, time.Time, bool) {
	h := a.hoy()
	desde := time.Date(h.Year(), h.Month(), 1, 0, 0, 0, 0, time.UTC)
	hasta := desde.AddDate(0, 1, -1)
	var err error
	if s := r.URL.Query().Get("desde"); s != "" {
		if desde, err = time.Parse(dateLayout, s); err != nil {
			httpx.Error(w, http.StatusBadRequest, "desde debe tener formato AAAA-MM-DD")
			return desde, hasta, false
		}
	}
	if s := r.URL.Query().Get("hasta"); s != "" {
		if hasta, err = time.Parse(dateLayout, s); err != nil {
			httpx.Error(w, http.StatusBadRequest, "hasta debe tener formato AAAA-MM-DD")
			return desde, hasta, false
		}
	}
	if desde.After(hasta) {
		httpx.Error(w, http.StatusBadRequest, "desde no puede ser posterior a hasta")
		return desde, hasta, false
	}
	return desde, hasta, true
}

// validarMovimiento valida los campos comunes de gastos e ingresos y devuelve la fecha.
func (a *API) validarMovimiento(desc string, monto float64, fecha string) (time.Time, string) {
	desc = strings.TrimSpace(desc)
	if desc == "" || len(desc) > 200 {
		return time.Time{}, "descripcion es obligatoria (máx. 200 caracteres)"
	}
	if monto <= 0 || monto > 9999999999 {
		return time.Time{}, "monto debe ser mayor a 0"
	}
	if fecha == "" {
		return a.hoy(), ""
	}
	f, err := time.Parse(dateLayout, fecha)
	if err != nil {
		return time.Time{}, "fecha debe tener formato AAAA-MM-DD"
	}
	return f, ""
}

func nilIfEmpty(s *string) *string {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil
	}
	return s
}

// categoriaEsDelUsuario evita que alguien asigne la categoría de otro usuario.
func (a *API) categoriaEsDelUsuario(ctx context.Context, uid string, catID *string) (bool, error) {
	if catID == nil {
		return true, nil
	}
	var ok bool
	err := a.DB.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM categorias WHERE id = $1 AND usuario_id = $2)`,
		*catID, uid).Scan(&ok)
	return ok, err
}
