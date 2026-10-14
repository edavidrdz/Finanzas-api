package handlers

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"finanzas-api/internal/auth"
	"finanzas-api/internal/httpx"
)

type IngresoProgramado struct {
	ID                  string  `json:"id"`
	CategoriaID         *string `json:"categoria_id"`
	Categoria           *string `json:"categoria,omitempty"`
	CuentaID            *string `json:"cuenta_id"`
	Cuenta              *string `json:"cuenta,omitempty"`
	Nombre              string  `json:"nombre"`
	Monto               float64 `json:"monto"`
	FechaProximoIngreso string  `json:"fecha_proximo_ingreso"`
	Frecuencia          string  `json:"frecuencia"`
	Certeza             string  `json:"certeza"`
	Activo              bool    `json:"activo"`
	MontoAsignado       float64 `json:"monto_asignado"`
}

type ingresoProgramadoInput struct {
	CategoriaID         *string `json:"categoria_id"`
	CuentaID            *string `json:"cuenta_id"`
	Nombre              string  `json:"nombre"`
	Monto               float64 `json:"monto"`
	FechaProximoIngreso string  `json:"fecha_proximo_ingreso"`
	Frecuencia          string  `json:"frecuencia"`
	Certeza             string  `json:"certeza"`
	Activo              *bool   `json:"activo"`
}

type RecibirIngresoProgramadoInput struct {
	FechaProgramada string   `json:"fecha_programada"`
	Fecha           string   `json:"fecha"`
	Monto           *float64 `json:"monto"`
}

type AsignacionIngreso struct {
	ID                     string  `json:"id"`
	IngresoProgramadoID    string  `json:"ingreso_programado_id"`
	NombreIngreso          string  `json:"nombre_ingreso"`
	FechaIngresoProgramada string  `json:"fecha_ingreso_programada"`
	GastoProgramadoID      string  `json:"gasto_programado_id"`
	NombreGasto            string  `json:"nombre_gasto"`
	FechaGastoProgramada   string  `json:"fecha_gasto_programada"`
	Monto                  float64 `json:"monto"`
}

type asignacionIngresoInput struct {
	FechaIngresoProgramada string  `json:"fecha_ingreso_programada"`
	GastoProgramadoID      string  `json:"gasto_programado_id"`
	FechaGastoProgramada   string  `json:"fecha_gasto_programada"`
	Monto                  float64 `json:"monto"`
}

func frecuenciaIngresoProgramadoValida(frecuencia string) bool {
	switch frecuencia {
	case "unico", "semanal", "quincenal", "mensual", "trimestral", "semestral", "anual":
		return true
	default:
		return false
	}
}

func (a *API) validarIngresoProgramado(r *http.Request, uid string, in *ingresoProgramadoInput) (string, error) {
	in.Nombre = strings.TrimSpace(in.Nombre)
	in.CategoriaID = nilIfEmpty(in.CategoriaID)
	in.CuentaID = nilIfEmpty(in.CuentaID)
	if in.Nombre == "" || len(in.Nombre) > 100 {
		return "nombre es obligatorio (máx. 100 caracteres)", nil
	}
	if in.Monto <= 0 || in.Monto > 9999999999 {
		return "monto debe ser mayor a 0", nil
	}
	if _, err := time.Parse(dateLayout, in.FechaProximoIngreso); err != nil {
		return "fecha_proximo_ingreso debe tener formato AAAA-MM-DD", nil
	}
	if !frecuenciaIngresoProgramadoValida(in.Frecuencia) {
		return "frecuencia no válida", nil
	}
	if in.Certeza == "" {
		in.Certeza = "estimado"
	}
	if in.Certeza != "estimado" && in.Certeza != "confirmado" {
		return "certeza debe ser estimado o confirmado", nil
	}
	ok, err := a.categoriaEsDelUsuario(r.Context(), uid, in.CategoriaID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "categoría inválida", nil
	}
	ok, err = a.cuentaAceptaIngreso(r.Context(), uid, in.CuentaID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "cuenta inválida para recibir ingresos", nil
	}
	return "", nil
}

func (a *API) ListIngresosProgramados(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	rows, err := a.DB.Query(r.Context(), `
		SELECT i.id::text, i.categoria_id::text, c.nombre, i.cuenta_id::text, cta.nombre,
		       i.nombre, i.monto::float8, to_char(i.fecha_proximo_ingreso, 'YYYY-MM-DD'),
		       i.frecuencia, i.certeza, i.activo,
		       COALESCE((
		           SELECT SUM(a.monto)
		           FROM asignaciones_ingresos_programados a
		           WHERE a.ingreso_programado_id = i.id
		             AND a.fecha_ingreso_programada = i.fecha_proximo_ingreso
		       ), 0)::float8
		FROM ingresos_programados i
		LEFT JOIN categorias c ON c.id = i.categoria_id
		LEFT JOIN cuentas cta ON cta.id = i.cuenta_id
		WHERE i.usuario_id = $1
		ORDER BY i.activo DESC, i.fecha_proximo_ingreso, i.nombre`, uid)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	defer rows.Close()
	out := []IngresoProgramado{}
	for rows.Next() {
		var i IngresoProgramado
		if err := rows.Scan(&i.ID, &i.CategoriaID, &i.Categoria, &i.CuentaID, &i.Cuenta,
			&i.Nombre, &i.Monto, &i.FechaProximoIngreso, &i.Frecuencia, &i.Certeza, &i.Activo,
			&i.MontoAsignado); err != nil {
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

func (a *API) CreateIngresoProgramado(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	var in ingresoProgramadoInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	msg, err := a.validarIngresoProgramado(r, uid, &in)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	if msg != "" {
		httpx.Error(w, http.StatusBadRequest, msg)
		return
	}
	fecha, _ := time.Parse(dateLayout, in.FechaProximoIngreso)
	activo := in.Activo == nil || *in.Activo
	out := IngresoProgramado{
		CategoriaID: in.CategoriaID, CuentaID: in.CuentaID, Nombre: in.Nombre, Monto: in.Monto,
		FechaProximoIngreso: in.FechaProximoIngreso, Frecuencia: in.Frecuencia, Certeza: in.Certeza, Activo: activo,
	}
	err = a.DB.QueryRow(r.Context(), `
		INSERT INTO ingresos_programados
		    (usuario_id, categoria_id, cuenta_id, nombre, monto, fecha_proximo_ingreso, dia_ingreso, frecuencia, certeza, activo)
		VALUES ($1, $2, $3, $4, $5::float8::numeric, $6, $7, $8, $9, $10)
		RETURNING id::text`,
		uid, in.CategoriaID, in.CuentaID, in.Nombre, in.Monto, fecha, fecha.Day(),
		in.Frecuencia, in.Certeza, activo).Scan(&out.ID)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, out)
}

func (a *API) UpdateIngresoProgramado(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	id := chi.URLParam(r, "id")
	var in ingresoProgramadoInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	msg, err := a.validarIngresoProgramado(r, uid, &in)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	if msg != "" {
		httpx.Error(w, http.StatusBadRequest, msg)
		return
	}
	fecha, _ := time.Parse(dateLayout, in.FechaProximoIngreso)
	activo := in.Activo == nil || *in.Activo
	tag, err := a.DB.Exec(r.Context(), `
		UPDATE ingresos_programados
		SET categoria_id = $3, cuenta_id = $4, nombre = $5, monto = $6::float8::numeric,
		    fecha_proximo_ingreso = $7, dia_ingreso = $8, frecuencia = $9, certeza = $10, activo = $11
		WHERE id = $1 AND usuario_id = $2`,
		id, uid, in.CategoriaID, in.CuentaID, in.Nombre, in.Monto, fecha, fecha.Day(),
		in.Frecuencia, in.Certeza, activo)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.Error(w, http.StatusNotFound, "no encontrado")
		return
	}
	httpx.JSON(w, http.StatusOK, IngresoProgramado{
		ID: id, CategoriaID: in.CategoriaID, CuentaID: in.CuentaID, Nombre: in.Nombre, Monto: in.Monto,
		FechaProximoIngreso: in.FechaProximoIngreso, Frecuencia: in.Frecuencia,
		Certeza: in.Certeza, Activo: activo,
	})
}

func (a *API) DeleteIngresoProgramado(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	tag, err := a.DB.Exec(r.Context(),
		`UPDATE ingresos_programados SET activo = false WHERE id = $1 AND usuario_id = $2`,
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

func (a *API) RecibirIngresoProgramado(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	id := chi.URLParam(r, "id")
	var in RecibirIngresoProgramadoInput
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
	if fecha.After(a.hoy()) {
		httpx.Error(w, http.StatusBadRequest, "la fecha recibida no puede ser futura")
		return
	}
	monto := 0.0
	if in.Monto != nil {
		monto = *in.Monto
	}
	tx, err := a.DB.Begin(r.Context())
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	defer tx.Rollback(r.Context())

	var i IngresoProgramado
	var dia int
	err = tx.QueryRow(r.Context(), `
		SELECT id::text, categoria_id::text, cuenta_id::text, nombre, monto::float8,
		       to_char(fecha_proximo_ingreso, 'YYYY-MM-DD'), dia_ingreso::int, frecuencia, certeza, activo
		FROM ingresos_programados
		WHERE id = $1 AND usuario_id = $2
		FOR UPDATE`, id, uid).Scan(&i.ID, &i.CategoriaID, &i.CuentaID, &i.Nombre, &i.Monto,
		&i.FechaProximoIngreso, &dia, &i.Frecuencia, &i.Certeza, &i.Activo)
	if err == pgx.ErrNoRows {
		httpx.Error(w, http.StatusNotFound, "ingreso programado no encontrado")
		return
	}
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	if !i.Activo || i.FechaProximoIngreso != in.FechaProgramada {
		httpx.Error(w, http.StatusConflict, "el ingreso esperado cambió o está inactivo; actualiza la lista")
		return
	}
	if in.Monto == nil {
		monto = i.Monto
	}
	if monto <= 0 || monto > 9999999999 {
		httpx.Error(w, http.StatusBadRequest, "monto debe ser mayor a 0")
		return
	}
	var montoAsignado float64
	err = tx.QueryRow(r.Context(), `
		SELECT COALESCE(SUM(monto), 0)::float8
		FROM asignaciones_ingresos_programados
		WHERE ingreso_programado_id = $1 AND fecha_ingreso_programada = $2`,
		id, fechaProgramada).Scan(&montoAsignado)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	if montoAsignado > monto+0.005 {
		httpx.Error(w, http.StatusConflict, "el ingreso recibido es menor que el monto asignado; ajusta las asignaciones antes de confirmarlo")
		return
	}
	periodicidad := periodicidadIngresoRecibido(i.Frecuencia)
	var ingresoID string
	err = tx.QueryRow(r.Context(), `
		INSERT INTO ingresos
		    (usuario_id, categoria_id, cuenta_id, ingreso_programado_id, programado_fecha, descripcion, monto, fecha, periodicidad)
		VALUES ($1, $2, $3, $4, $5, $6, $7::float8::numeric, $8, $9)
		RETURNING id::text`,
		uid, i.CategoriaID, i.CuentaID, id, fechaProgramada, i.Nombre, monto, fecha, periodicidad).Scan(&ingresoID)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	_, err = tx.Exec(r.Context(), `
		INSERT INTO programado_apartados
		    (programado_id, usuario_id, fecha_programada, monto, fecha, ingreso_programado_id, ingreso_fecha_programada)
		SELECT a.gasto_programado_id, a.usuario_id, a.fecha_gasto_programada, a.monto, $3, a.ingreso_programado_id, a.fecha_ingreso_programada
		FROM asignaciones_ingresos_programados a
		JOIN gastos_programados g ON g.id = a.gasto_programado_id AND g.usuario_id = a.usuario_id
		WHERE a.ingreso_programado_id = $1 AND a.fecha_ingreso_programada = $2
		  AND g.activo AND g.fecha_proximo_pago = a.fecha_gasto_programada`,
		id, fechaProgramada, fecha)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	if i.Frecuencia == "unico" {
		_, err = tx.Exec(r.Context(),
			`UPDATE ingresos_programados SET activo = false WHERE id = $1 AND usuario_id = $2`, id, uid)
	} else {
		siguiente, errNext := siguienteFechaIngreso(fechaProgramada, i.Frecuencia, dia)
		if errNext != nil {
			httpx.Error(w, http.StatusInternalServerError, "no se pudo calcular el próximo ingreso")
			return
		}
		_, err = tx.Exec(r.Context(), `
			UPDATE ingresos_programados SET fecha_proximo_ingreso = $3
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
		"ingreso_id": ingresoID, "fecha": fecha.Format(dateLayout),
		"fecha_programada": in.FechaProgramada, "monto": monto,
	})
}

func periodicidadIngresoRecibido(frecuencia string) string {
	switch frecuencia {
	case "semanal":
		return "semanal"
	case "quincenal":
		return "quincenal"
	case "mensual":
		return "mensual"
	default:
		return "unico"
	}
}

func siguienteFechaIngreso(actual time.Time, frecuencia string, diaIngreso int) (time.Time, error) {
	switch frecuencia {
	case "semanal":
		return actual.AddDate(0, 0, 7), nil
	case "quincenal":
		return actual.AddDate(0, 0, 14), nil
	case "mensual", "trimestral", "semestral", "anual":
		return siguienteFechaProgramada(actual, frecuencia, diaIngreso)
	default:
		return time.Time{}, errors.New("frecuencia no válida")
	}
}

func (a *API) ListAsignacionesIngreso(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	rows, err := a.DB.Query(r.Context(), `
		SELECT x.id::text, x.ingreso_programado_id::text, i.nombre,
		       to_char(x.fecha_ingreso_programada, 'YYYY-MM-DD'),
		       x.gasto_programado_id::text, g.nombre,
		       to_char(x.fecha_gasto_programada, 'YYYY-MM-DD'), x.monto::float8
		FROM asignaciones_ingresos_programados x
		JOIN ingresos_programados i ON i.id = x.ingreso_programado_id
		JOIN gastos_programados g ON g.id = x.gasto_programado_id
		WHERE x.ingreso_programado_id = $1 AND x.usuario_id = $2
		ORDER BY x.fecha_ingreso_programada DESC, x.creado_en DESC
		LIMIT 500`, chi.URLParam(r, "id"), uid)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	defer rows.Close()
	out := []AsignacionIngreso{}
	for rows.Next() {
		var x AsignacionIngreso
		if err := rows.Scan(&x.ID, &x.IngresoProgramadoID, &x.NombreIngreso,
			&x.FechaIngresoProgramada, &x.GastoProgramadoID, &x.NombreGasto,
			&x.FechaGastoProgramada, &x.Monto); err != nil {
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

func (a *API) CreateAsignacionIngreso(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	ingresoID := chi.URLParam(r, "id")
	var in asignacionIngresoInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	if in.Monto <= 0 || in.Monto > 9999999999 {
		httpx.Error(w, http.StatusBadRequest, "monto debe ser mayor a 0")
		return
	}
	fechaIngreso, err := time.Parse(dateLayout, in.FechaIngresoProgramada)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "fecha_ingreso_programada debe tener formato AAAA-MM-DD")
		return
	}
	fechaGasto, err := time.Parse(dateLayout, in.FechaGastoProgramada)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "fecha_gasto_programada debe tener formato AAAA-MM-DD")
		return
	}
	if ingresoID == "" || in.GastoProgramadoID == "" {
		httpx.Error(w, http.StatusBadRequest, "selecciona un ingreso y un gasto programados")
		return
	}
	if fechaIngreso.After(fechaGasto) {
		httpx.Error(w, http.StatusBadRequest, "la fecha esperada del ingreso debe ser anterior o igual al vencimiento del gasto")
		return
	}
	tx, err := a.DB.Begin(r.Context())
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	defer tx.Rollback(r.Context())

	var ingresoMonto float64
	var ingresoActual string
	var nombreIngreso string
	err = tx.QueryRow(r.Context(), `
		SELECT monto::float8, to_char(fecha_proximo_ingreso, 'YYYY-MM-DD'), nombre
		FROM ingresos_programados
		WHERE id = $1 AND usuario_id = $2 AND activo
		FOR UPDATE`, ingresoID, uid).Scan(&ingresoMonto, &ingresoActual, &nombreIngreso)
	if err == pgx.ErrNoRows {
		httpx.Error(w, http.StatusNotFound, "ingreso programado no encontrado o inactivo")
		return
	}
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	if ingresoActual != in.FechaIngresoProgramada {
		httpx.Error(w, http.StatusConflict, "la fecha del ingreso cambió; actualiza la lista")
		return
	}
	var gastoMonto float64
	var gastoActual string
	var nombreGasto string
	err = tx.QueryRow(r.Context(), `
		SELECT monto::float8, to_char(fecha_proximo_pago, 'YYYY-MM-DD'), nombre
		FROM gastos_programados
		WHERE id = $1 AND usuario_id = $2 AND activo
		FOR UPDATE`, in.GastoProgramadoID, uid).Scan(&gastoMonto, &gastoActual, &nombreGasto)
	if err == pgx.ErrNoRows {
		httpx.Error(w, http.StatusNotFound, "gasto programado no encontrado o inactivo")
		return
	}
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	if gastoActual != in.FechaGastoProgramada {
		httpx.Error(w, http.StatusConflict, "la fecha del gasto cambió; actualiza la lista")
		return
	}

	var asignadoIngreso, asignadoGasto, apartadoGasto float64
	err = tx.QueryRow(r.Context(), `
		SELECT
			COALESCE((
				SELECT SUM(monto) FROM asignaciones_ingresos_programados
				WHERE ingreso_programado_id = $1 AND fecha_ingreso_programada = $2
			), 0)::float8,
			COALESCE((
				SELECT SUM(monto) FROM asignaciones_ingresos_programados
				WHERE gasto_programado_id = $3 AND fecha_gasto_programada = $4
			), 0)::float8,
			COALESCE((
				SELECT SUM(monto) FROM programado_apartados
				WHERE programado_id = $3 AND fecha_programada = $4
			), 0)::float8`,
		ingresoID, fechaIngreso, in.GastoProgramadoID, fechaGasto,
	).Scan(&asignadoIngreso, &asignadoGasto, &apartadoGasto)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	if asignadoIngreso+in.Monto > ingresoMonto+0.005 {
		httpx.Error(w, http.StatusBadRequest, "la asignación supera el monto esperado del ingreso")
		return
	}
	if asignadoGasto+apartadoGasto+in.Monto > gastoMonto+0.005 {
		httpx.Error(w, http.StatusBadRequest, "las asignaciones y apartados superan la meta del gasto")
		return
	}

	var out AsignacionIngreso
	err = tx.QueryRow(r.Context(), `
		INSERT INTO asignaciones_ingresos_programados
		    (usuario_id, ingreso_programado_id, fecha_ingreso_programada, gasto_programado_id, fecha_gasto_programada, monto)
		VALUES ($1, $2, $3, $4, $5, $6::float8::numeric)
		ON CONFLICT ON CONSTRAINT uq_asignacion_ingreso_gasto_ciclo
		DO UPDATE SET monto = asignaciones_ingresos_programados.monto + EXCLUDED.monto
		RETURNING id::text, ingreso_programado_id::text, to_char(fecha_ingreso_programada, 'YYYY-MM-DD'),
		          gasto_programado_id::text, to_char(fecha_gasto_programada, 'YYYY-MM-DD'), monto::float8`,
		uid, ingresoID, fechaIngreso, in.GastoProgramadoID, fechaGasto, in.Monto,
	).Scan(&out.ID, &out.IngresoProgramadoID, &out.FechaIngresoProgramada,
		&out.GastoProgramadoID, &out.FechaGastoProgramada, &out.Monto)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpx.DBError(w, err)
		return
	}
	out.NombreIngreso = nombreIngreso
	out.NombreGasto = nombreGasto
	httpx.JSON(w, http.StatusCreated, out)
}

func (a *API) DeleteAsignacionIngreso(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	ingresoID := chi.URLParam(r, "id")
	asignacionID := chi.URLParam(r, "asignacionId")
	tx, err := a.DB.Begin(r.Context())
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var lockedID string
	err = tx.QueryRow(r.Context(), `
		SELECT id::text FROM ingresos_programados
		WHERE id = $1 AND usuario_id = $2
		FOR UPDATE`, ingresoID, uid).Scan(&lockedID)
	if err == pgx.ErrNoRows {
		httpx.Error(w, http.StatusNotFound, "ingreso programado no encontrado")
		return
	}
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	var fechaProgramada time.Time
	err = tx.QueryRow(r.Context(), `
		SELECT fecha_ingreso_programada
		FROM asignaciones_ingresos_programados
		WHERE id = $1 AND ingreso_programado_id = $2 AND usuario_id = $3
		FOR UPDATE`, asignacionID, ingresoID, uid).Scan(&fechaProgramada)
	if err == pgx.ErrNoRows {
		httpx.Error(w, http.StatusNotFound, "asignación no encontrada")
		return
	}
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	var recibido bool
	err = tx.QueryRow(r.Context(), `
		SELECT EXISTS (
			SELECT 1 FROM ingresos
			WHERE ingreso_programado_id = $1 AND programado_fecha = $2 AND usuario_id = $3
		)`, ingresoID, fechaProgramada, uid).Scan(&recibido)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	if recibido {
		httpx.Error(w, http.StatusConflict, "no se puede borrar una asignación ya convertida en apartado")
		return
	}
	_, err = tx.Exec(r.Context(), `
		DELETE FROM asignaciones_ingresos_programados
		WHERE id = $1 AND ingreso_programado_id = $2 AND usuario_id = $3`,
		asignacionID, ingresoID, uid)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpx.DBError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
