package handlers

import (
	"errors"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"finanzas-api/internal/auth"
	"finanzas-api/internal/httpx"
)

type Programado struct {
	ID                         string  `json:"id"`
	CategoriaID                *string `json:"categoria_id"`
	Categoria                  *string `json:"categoria,omitempty"`
	CuentaID                   *string `json:"cuenta_id"`
	Cuenta                     *string `json:"cuenta,omitempty"`
	Nombre                     string  `json:"nombre"`
	Monto                      float64 `json:"monto"`
	FechaProximoPago           string  `json:"fecha_proximo_pago"`
	Frecuencia                 string  `json:"frecuencia"`
	Activo                     bool    `json:"activo"`
	ApartadoActual             float64 `json:"apartado_actual"`
	IngresosEstimadosAsignados float64 `json:"ingresos_estimados_asignados"`
	AportacionSugerida         float64 `json:"aportacion_mensual_sugerida"`
	DiasRestantes              int     `json:"dias_restantes"`
}

type programadoInput struct {
	CategoriaID      *string `json:"categoria_id"`
	CuentaID         *string `json:"cuenta_id"`
	Nombre           string  `json:"nombre"`
	Monto            float64 `json:"monto"`
	FechaProximoPago string  `json:"fecha_proximo_pago"`
	Frecuencia       string  `json:"frecuencia"`
	Activo           *bool   `json:"activo"`
}

type ProgramadoApartado struct {
	ID              string  `json:"id"`
	ProgramadoID    string  `json:"programado_id"`
	FechaProgramada string  `json:"fecha_programada"`
	Monto           float64 `json:"monto"`
	Fecha           string  `json:"fecha"`
}

type programadoApartadoInput struct {
	FechaProgramada string  `json:"fecha_programada"`
	Monto           float64 `json:"monto"`
	Fecha           string  `json:"fecha"`
}

type pagarProgramadoInput struct {
	FechaProgramada string   `json:"fecha_programada"`
	Monto           *float64 `json:"monto"`
	Fecha           string   `json:"fecha"`
}

func frecuenciaProgramadoValida(frecuencia string) bool {
	switch frecuencia {
	case "unico", "mensual", "trimestral", "semestral", "anual":
		return true
	default:
		return false
	}
}

func (a *API) validarProgramado(r *http.Request, uid string, in *programadoInput) (string, error) {
	in.Nombre = strings.TrimSpace(in.Nombre)
	in.CategoriaID = nilIfEmpty(in.CategoriaID)
	in.CuentaID = nilIfEmpty(in.CuentaID)
	if in.Nombre == "" || len(in.Nombre) > 100 {
		return "nombre es obligatorio (máx. 100 caracteres)", nil
	}
	if in.Monto <= 0 || in.Monto > 9999999999 {
		return "monto debe ser mayor a 0", nil
	}
	if _, err := time.Parse(dateLayout, in.FechaProximoPago); err != nil {
		return "fecha_proximo_pago debe tener formato AAAA-MM-DD", nil
	}
	if !frecuenciaProgramadoValida(in.Frecuencia) {
		return "frecuencia debe ser unico, mensual, trimestral, semestral o anual", nil
	}
	ok, err := a.categoriaEsDelUsuario(r.Context(), uid, in.CategoriaID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "categoría inválida", nil
	}
	ok, err = a.cuentaEsDelUsuario(r.Context(), uid, in.CuentaID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "cuenta inválida", nil
	}
	return "", nil
}

func aportacionSugerida(monto, apartado float64, fechaProgramada string, hoy time.Time) (float64, int) {
	fecha, err := time.Parse(dateLayout, fechaProgramada)
	if err != nil {
		return 0, 0
	}
	dias := int(fecha.Sub(hoy).Hours() / 24)
	restante := monto - apartado
	if restante < 0 {
		restante = 0
	}
	meses := int(math.Ceil(float64(dias) / 30.4375))
	if meses < 1 {
		meses = 1
	}
	sugerida := float64(int((restante/float64(meses))*100+0.5)) / 100
	return sugerida, dias
}

func (a *API) ListProgramados(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	rows, err := a.DB.Query(r.Context(), `
		SELECT p.id::text, p.categoria_id::text, c.nombre, p.cuenta_id::text, cta.nombre,
		       p.nombre, p.monto::float8, to_char(p.fecha_proximo_pago, 'YYYY-MM-DD'),
		       p.frecuencia, p.activo,
		       COALESCE((
		           SELECT SUM(ap.monto)
		           FROM programado_apartados ap
		           WHERE ap.programado_id = p.id AND ap.fecha_programada = p.fecha_proximo_pago
		              ), 0)::float8,
		              COALESCE((
		                  SELECT SUM(ai.monto)
		                  FROM asignaciones_ingresos_programados ai
		                  WHERE ai.gasto_programado_id = p.id AND ai.fecha_gasto_programada = p.fecha_proximo_pago
		              ), 0)::float8
		FROM gastos_programados p
		LEFT JOIN categorias c ON c.id = p.categoria_id
		LEFT JOIN cuentas cta ON cta.id = p.cuenta_id
		WHERE p.usuario_id = $1
		ORDER BY p.activo DESC, p.fecha_proximo_pago, p.nombre`, uid)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	defer rows.Close()
	out := []Programado{}
	hoy := a.hoy()
	for rows.Next() {
		var p Programado
		if err := rows.Scan(&p.ID, &p.CategoriaID, &p.Categoria, &p.CuentaID, &p.Cuenta,
			&p.Nombre, &p.Monto, &p.FechaProximoPago, &p.Frecuencia, &p.Activo,
			&p.ApartadoActual, &p.IngresosEstimadosAsignados); err != nil {
			httpx.DBError(w, err)
			return
		}
		p.AportacionSugerida, p.DiasRestantes = aportacionSugerida(p.Monto, p.ApartadoActual, p.FechaProximoPago, hoy)
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		httpx.DBError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (a *API) CreateProgramado(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	var in programadoInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	msg, err := a.validarProgramado(r, uid, &in)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	if msg != "" {
		httpx.Error(w, http.StatusBadRequest, msg)
		return
	}
	activo := in.Activo == nil || *in.Activo
	fechaProximoPago, _ := time.Parse(dateLayout, in.FechaProximoPago)
	p := Programado{
		CategoriaID: in.CategoriaID, CuentaID: in.CuentaID, Nombre: in.Nombre, Monto: in.Monto,
		FechaProximoPago: in.FechaProximoPago, Frecuencia: in.Frecuencia, Activo: activo,
	}
	err = a.DB.QueryRow(r.Context(), `
		INSERT INTO gastos_programados
		    (usuario_id, categoria_id, cuenta_id, nombre, monto, fecha_proximo_pago, dia_vencimiento, frecuencia, activo)
		VALUES ($1, $2, $3, $4, $5::float8::numeric, $6, $7, $8, $9)
		RETURNING id::text`,
		uid, in.CategoriaID, in.CuentaID, in.Nombre, in.Monto, in.FechaProximoPago, fechaProximoPago.Day(), in.Frecuencia, activo,
	).Scan(&p.ID)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	p.AportacionSugerida, p.DiasRestantes = aportacionSugerida(p.Monto, 0, p.FechaProximoPago, a.hoy())
	httpx.JSON(w, http.StatusCreated, p)
}

func (a *API) UpdateProgramado(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	id := chi.URLParam(r, "id")
	var in programadoInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	msg, err := a.validarProgramado(r, uid, &in)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	if msg != "" {
		httpx.Error(w, http.StatusBadRequest, msg)
		return
	}
	activo := in.Activo == nil || *in.Activo
	fechaProximoPago, _ := time.Parse(dateLayout, in.FechaProximoPago)
	tx, err := a.DB.Begin(r.Context())
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	defer tx.Rollback(r.Context())

	var lockedID string
	err = tx.QueryRow(r.Context(), `
		SELECT id::text FROM gastos_programados WHERE id = $1 AND usuario_id = $2 FOR UPDATE`,
		id, uid).Scan(&lockedID)
	if err == pgx.ErrNoRows {
		httpx.Error(w, http.StatusNotFound, "no encontrado")
		return
	}
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	var yaPagado bool
	err = tx.QueryRow(r.Context(), `
		SELECT EXISTS (
			SELECT 1 FROM gastos
			WHERE programado_id = $1 AND usuario_id = $2 AND programado_fecha = $3
		)`, id, uid, fechaProximoPago).Scan(&yaPagado)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	if activo && yaPagado {
		httpx.Error(w, http.StatusConflict, "ese vencimiento ya fue pagado; cambia la fecha para programar uno nuevo")
		return
	}
	_, err = tx.Exec(r.Context(), `
		UPDATE gastos_programados
		SET categoria_id = $3, cuenta_id = $4, nombre = $5, monto = $6::float8::numeric,
		    fecha_proximo_pago = $7, dia_vencimiento = $8, frecuencia = $9, activo = $10
		WHERE id = $1 AND usuario_id = $2`,
		id, uid, in.CategoriaID, in.CuentaID, in.Nombre, in.Monto,
		in.FechaProximoPago, fechaProximoPago.Day(), in.Frecuencia, activo)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpx.DBError(w, err)
		return
	}
	p := Programado{
		ID: id, CategoriaID: in.CategoriaID, CuentaID: in.CuentaID, Nombre: in.Nombre, Monto: in.Monto,
		FechaProximoPago: in.FechaProximoPago, Frecuencia: in.Frecuencia, Activo: activo,
	}
	p.AportacionSugerida, p.DiasRestantes = aportacionSugerida(p.Monto, 0, p.FechaProximoPago, a.hoy())
	httpx.JSON(w, http.StatusOK, p)
}

func (a *API) DeleteProgramado(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	tag, err := a.DB.Exec(r.Context(),
		`UPDATE gastos_programados SET activo = false WHERE id = $1 AND usuario_id = $2`,
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

func (a *API) ListProgramadoApartados(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	rows, err := a.DB.Query(r.Context(), `
		SELECT ap.id::text, ap.programado_id::text, to_char(ap.fecha_programada, 'YYYY-MM-DD'),
		       ap.monto::float8, to_char(ap.fecha, 'YYYY-MM-DD')
		FROM programado_apartados ap
		JOIN gastos_programados p ON p.id = ap.programado_id
		WHERE p.id = $1 AND p.usuario_id = $2
		ORDER BY ap.fecha DESC, ap.creado_en DESC
		LIMIT 500`, chi.URLParam(r, "id"), uid)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	defer rows.Close()
	out := []ProgramadoApartado{}
	for rows.Next() {
		var x ProgramadoApartado
		if err := rows.Scan(&x.ID, &x.ProgramadoID, &x.FechaProgramada, &x.Monto, &x.Fecha); err != nil {
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

func (a *API) CreateProgramadoApartado(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	id := chi.URLParam(r, "id")
	var in programadoApartadoInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	if in.Monto <= 0 || in.Monto > 9999999999 {
		httpx.Error(w, http.StatusBadRequest, "monto debe ser mayor a 0")
		return
	}
	fechaProgramada, err := time.Parse(dateLayout, in.FechaProgramada)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "fecha_programada debe tener formato AAAA-MM-DD")
		return
	}
	fecha := a.hoy()
	if in.Fecha != "" {
		fecha, err = time.Parse(dateLayout, in.Fecha)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "fecha debe tener formato AAAA-MM-DD")
			return
		}
	}
	tx, err := a.DB.Begin(r.Context())
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var actual string
	err = tx.QueryRow(r.Context(), `
		SELECT to_char(fecha_proximo_pago, 'YYYY-MM-DD')
		FROM gastos_programados
		WHERE id = $1 AND usuario_id = $2 AND activo
		FOR UPDATE`, id, uid).Scan(&actual)
	if err == pgx.ErrNoRows {
		httpx.Error(w, http.StatusNotFound, "gasto programado no encontrado o inactivo")
		return
	}
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	if actual != in.FechaProgramada {
		httpx.Error(w, http.StatusConflict, "la fecha programada cambió; actualiza la lista antes de apartar")
		return
	}
	var apartado ProgramadoApartado
	err = tx.QueryRow(r.Context(), `
		INSERT INTO programado_apartados (programado_id, usuario_id, fecha_programada, monto, fecha)
		VALUES ($1, $2, $3, $4::float8::numeric, $5)
		RETURNING id::text, programado_id::text, to_char(fecha_programada, 'YYYY-MM-DD'),
		          monto::float8, to_char(fecha, 'YYYY-MM-DD')`,
		id, uid, fechaProgramada, in.Monto, fecha,
	).Scan(&apartado.ID, &apartado.ProgramadoID, &apartado.FechaProgramada, &apartado.Monto, &apartado.Fecha)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpx.DBError(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, apartado)
}

func (a *API) PagarProgramado(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	id := chi.URLParam(r, "id")
	var in pagarProgramadoInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	fechaProgramada, err := time.Parse(dateLayout, in.FechaProgramada)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "fecha_programada debe tener formato AAAA-MM-DD")
		return
	}
	fecha := a.hoy()
	if in.Fecha != "" {
		fecha, err = time.Parse(dateLayout, in.Fecha)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "fecha debe tener formato AAAA-MM-DD")
			return
		}
	}
	tx, err := a.DB.Begin(r.Context())
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	defer tx.Rollback(r.Context())

	var p Programado
	var diaVencimiento int
	err = tx.QueryRow(r.Context(), `
		SELECT id::text, categoria_id::text, cuenta_id::text, nombre, monto::float8,
		       to_char(fecha_proximo_pago, 'YYYY-MM-DD'), dia_vencimiento::int, frecuencia, activo
		FROM gastos_programados
		WHERE id = $1 AND usuario_id = $2
		FOR UPDATE`, id, uid).Scan(&p.ID, &p.CategoriaID, &p.CuentaID, &p.Nombre, &p.Monto,
		&p.FechaProximoPago, &diaVencimiento, &p.Frecuencia, &p.Activo)
	if err == pgx.ErrNoRows {
		httpx.Error(w, http.StatusNotFound, "gasto programado no encontrado o inactivo")
		return
	}
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	if !p.Activo {
		httpx.Error(w, http.StatusNotFound, "gasto programado no encontrado o inactivo")
		return
	}
	if p.FechaProximoPago != in.FechaProgramada {
		httpx.Error(w, http.StatusConflict, "este vencimiento ya se registró o cambió; actualiza la lista")
		return
	}
	monto := p.Monto
	if in.Monto != nil {
		monto = *in.Monto
	}
	if monto <= 0 || monto > 9999999999 {
		httpx.Error(w, http.StatusBadRequest, "monto debe ser mayor a 0")
		return
	}
	var gastoID string
	err = tx.QueryRow(r.Context(), `
		INSERT INTO gastos
		    (usuario_id, categoria_id, cuenta_id, programado_id, programado_fecha, descripcion, monto, fecha)
		VALUES ($1, $2, $3, $4, $5, $6, $7::float8::numeric, $8)
		RETURNING id::text`,
		uid, p.CategoriaID, p.CuentaID, id, fechaProgramada, p.Nombre, monto, fecha,
	).Scan(&gastoID)
	if err != nil {
		httpx.DBError(w, err)
		return
	}

	if p.Frecuencia == "unico" {
		_, err = tx.Exec(r.Context(),
			`UPDATE gastos_programados SET activo = false WHERE id = $1 AND usuario_id = $2`, id, uid)
	} else {
		siguiente, errNext := siguienteFechaProgramada(fechaProgramada, p.Frecuencia, diaVencimiento)
		if errNext != nil {
			httpx.Error(w, http.StatusInternalServerError, "no se pudo calcular el próximo vencimiento")
			return
		}
		_, err = tx.Exec(r.Context(), `
			UPDATE gastos_programados SET fecha_proximo_pago = $3
			WHERE id = $1 AND usuario_id = $2`, id, uid, siguiente)
	}
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpx.DBError(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{
		"gasto_id": gastoID, "fecha": fecha.Format(dateLayout), "monto": monto,
		"fecha_programada": in.FechaProgramada,
	})
}

func siguienteFechaProgramada(actual time.Time, frecuencia string, diaVencimiento int) (time.Time, error) {
	meses := 0
	switch frecuencia {
	case "mensual":
		meses = 1
	case "trimestral":
		meses = 3
	case "semestral":
		meses = 6
	case "anual":
		meses = 12
	default:
		return time.Time{}, errors.New("frecuencia no válida")
	}
	primerDia := time.Date(actual.Year(), actual.Month()+time.Month(meses), 1, 0, 0, 0, 0, time.UTC)
	dia := diaVencimiento
	ultimoDia := primerDia.AddDate(0, 1, -1).Day()
	if dia > ultimoDia {
		dia = ultimoDia
	}
	return time.Date(primerDia.Year(), primerDia.Month(), dia, 0, 0, 0, 0, time.UTC), nil
}
