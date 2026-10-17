package handlers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"finanzas-api/internal/auth"
	"finanzas-api/internal/httpx"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type syncEntity struct {
	table  string
	fields []string
	filter string
	refs   map[string]string
}

var syncEntities = map[string]syncEntity{
	"categories": {
		table: "categorias", fields: []string{"nombre", "tipo"},
	},
	"accounts": {
		table: "cuentas", fields: []string{"nombre", "institucion", "tipo", "saldo_inicial", "fecha_saldo_inicial", "limite_credito", "activa"},
	},
	"expenses": {
		table: "gastos", fields: []string{"categoria_id", "recurrente_id", "descripcion", "monto", "fecha", "cuenta_id", "programado_id", "programado_fecha", "ubicacion_latitud", "ubicacion_longitud", "ubicacion_nombre"},
		refs: map[string]string{"categoria_id": "categorias", "recurrente_id": "gastos_recurrentes", "cuenta_id": "cuentas", "programado_id": "gastos_programados"},
	},
	"expense_drafts": {
		table: "gastos_pendientes", fields: []string{"categoria_id", "cuenta_id", "descripcion", "monto", "fecha", "ubicacion_latitud", "ubicacion_longitud", "ubicacion_nombre", "iniciada_en", "completando"},
		refs: map[string]string{"categoria_id": "categorias", "cuenta_id": "cuentas"},
	},
	"incomes": {
		table: "ingresos", fields: []string{"categoria_id", "descripcion", "monto", "fecha", "periodicidad", "cuenta_id", "ingreso_programado_id", "programado_fecha"},
		refs: map[string]string{"categoria_id": "categorias", "cuenta_id": "cuentas", "ingreso_programado_id": "ingresos_programados"},
	},
	"transfers": {
		table: "transferencias", fields: []string{"cuenta_origen_id", "cuenta_destino_id", "descripcion", "monto", "fecha"},
		refs: map[string]string{"cuenta_origen_id": "cuentas", "cuenta_destino_id": "cuentas"},
	},
	"recurring_services": {
		table: "gastos_recurrentes", fields: []string{"categoria_id", "cuenta_id", "nombre", "monto", "dia_vencimiento", "activo"},
		refs: map[string]string{"categoria_id": "categorias", "cuenta_id": "cuentas"},
	},
	"scheduled_expenses": {
		table: "gastos_programados", fields: []string{"categoria_id", "cuenta_id", "nombre", "monto", "fecha_proximo_pago", "dia_vencimiento", "frecuencia", "activo"},
		refs: map[string]string{"categoria_id": "categorias", "cuenta_id": "cuentas"},
	},
	"scheduled_expense_allocations": {
		table: "programado_apartados", fields: []string{"programado_id", "fecha_programada", "monto", "fecha", "ingreso_programado_id", "ingreso_fecha_programada"},
		refs: map[string]string{"programado_id": "gastos_programados", "ingreso_programado_id": "ingresos_programados"},
	},
	"scheduled_expense_payments": {
		table: "gastos", fields: []string{"categoria_id", "recurrente_id", "descripcion", "monto", "fecha", "cuenta_id", "programado_id", "programado_fecha"},
		filter: "programado_id IS NOT NULL",
		refs:   map[string]string{"categoria_id": "categorias", "recurrente_id": "gastos_recurrentes", "cuenta_id": "cuentas", "programado_id": "gastos_programados"},
	},
	"scheduled_incomes": {
		table: "ingresos_programados", fields: []string{"categoria_id", "cuenta_id", "nombre", "monto", "fecha_proximo_ingreso", "dia_ingreso", "frecuencia", "certeza", "activo"},
		refs: map[string]string{"categoria_id": "categorias", "cuenta_id": "cuentas"},
	},
	"scheduled_income_assignments": {
		table: "asignaciones_ingresos_programados", fields: []string{"ingreso_programado_id", "fecha_ingreso_programada", "gasto_programado_id", "fecha_gasto_programada", "monto"},
		refs: map[string]string{"ingreso_programado_id": "ingresos_programados", "gasto_programado_id": "gastos_programados"},
	},
	"balance_adjustments": {
		table: "ajustes_saldo", fields: []string{"cuenta_id", "monto", "fecha", "motivo"},
		refs: map[string]string{"cuenta_id": "cuentas"},
	},
}

var syncUUIDPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type syncOperation struct {
	OpID       string          `json:"op_id"`
	Entity     string          `json:"entity"`
	Action     string          `json:"action"`
	EntityID   string          `json:"entity_id"`
	Payload    json.RawMessage `json:"payload"`
	BaseRecord json.RawMessage `json:"base_record"`
}

type syncResult struct {
	OpID     string `json:"op_id"`
	Status   string `json:"status"`
	Entity   string `json:"entity"`
	EntityID string `json:"entity_id"`
	Error    string `json:"error,omitempty"`
}

// GetSyncSnapshot returns the current user-owned data and deletion tombstones.
func (a *API) GetSyncSnapshot(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tx, err := a.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	defer tx.Rollback(ctx)

	uid := auth.UserID(ctx)
	snapshot := make(map[string]any, len(syncEntities)+1)
	names := make([]string, 0, len(syncEntities))
	for name := range syncEntities {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		entity := syncEntities[name]
		where := "usuario_id = $1"
		if entity.filter != "" {
			where += " AND " + entity.filter
		}
		var collection json.RawMessage
		if name == "accounts" {
			err = tx.QueryRow(ctx, `
				SELECT COALESCE(jsonb_agg(
					(to_jsonb(c) - 'usuario_id' - 'creado_en')
					|| jsonb_build_object('saldo_actual', (`+cuentaBalancesSQL+`)::float8)
					ORDER BY c.id), '[]'::jsonb)
				FROM cuentas c WHERE c.usuario_id = $1`, uid).Scan(&collection)
		} else {
			err = tx.QueryRow(ctx,
				fmt.Sprintf(`SELECT COALESCE(jsonb_agg(to_jsonb(t) - 'usuario_id' - 'creado_en' ORDER BY t.id), '[]'::jsonb) FROM %s t WHERE %s`, entity.table, qualifySyncWhere(where)),
				uid,
			).Scan(&collection)
		}
		if err != nil {
			httpx.DBError(w, err)
			return
		}
		snapshot[name] = collection
	}
	var deletions json.RawMessage
	err = tx.QueryRow(ctx, `
		SELECT COALESCE(jsonb_agg(jsonb_build_object(
			'entity', entidad, 'entity_id', entidad_id, 'deleted_at', eliminado_en
		) ORDER BY id), '[]'::jsonb)
		FROM sync_tombstones WHERE usuario_id = $1`, uid).Scan(&deletions)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	snapshot["remote_deletions"] = deletions
	if err := tx.Commit(ctx); err != nil {
		httpx.DBError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, snapshot)
}

func qualifySyncWhere(where string) string {
	return strings.ReplaceAll(where, "usuario_id", "t.usuario_id")
}

// PushSync applies independently validated operations from an offline outbox.
func (a *API) PushSync(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Operations []syncOperation `json:"operations"`
	}
	if !httpx.Decode(w, r, &body) {
		return
	}
	if len(body.Operations) == 0 || len(body.Operations) > 100 {
		httpx.Error(w, http.StatusBadRequest, "operations debe contener entre 1 y 100 elementos")
		return
	}
	ctx := r.Context()
	uid := auth.UserID(ctx)
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	defer tx.Rollback(ctx)

	results := make([]syncResult, 0, len(body.Operations))
	for index, operation := range body.Operations {
		if _, err := tx.Exec(ctx, fmt.Sprintf("SAVEPOINT sync_op_%d", index)); err != nil {
			httpx.DBError(w, err)
			return
		}
		result, fingerprint, cacheable := a.applySyncOperation(ctx, tx, uid, operation)
		if result.Status == "applied" || result.Status == "duplicate" {
			if cacheable {
				if _, err := tx.Exec(ctx, `
					INSERT INTO sync_operations (usuario_id, op_id, fingerprint, result)
					VALUES ($1, $2, $3, $4::jsonb)
					ON CONFLICT (usuario_id, op_id) DO NOTHING`,
					uid, operation.OpID, fingerprint, mustJSON(result)); err != nil {
					result = syncResult{OpID: operation.OpID, Status: "rejected", Entity: operation.Entity, EntityID: operation.EntityID, Error: "no se pudo guardar la confirmación"}
					cacheable = false
				}
			}
		}
		if result.Status == "conflict" || result.Status == "rejected" {
			_, _ = tx.Exec(ctx, fmt.Sprintf("ROLLBACK TO SAVEPOINT sync_op_%d", index))
		}
		_, _ = tx.Exec(ctx, fmt.Sprintf("RELEASE SAVEPOINT sync_op_%d", index))
		results = append(results, result)
	}
	if err := tx.Commit(ctx); err != nil {
		httpx.DBError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"results": results})
}

func (a *API) applySyncOperation(ctx context.Context, tx pgx.Tx, uid string, op syncOperation) (syncResult, string, bool) {
	result := syncResult{OpID: op.OpID, Entity: op.Entity, EntityID: op.EntityID}
	fingerprint := syncFingerprint(op)
	if !syncUUIDPattern.MatchString(op.OpID) || !syncUUIDPattern.MatchString(op.EntityID) {
		return rejectSync(result, "op_id y entity_id deben ser UUID"), fingerprint, false
	}
	entity, ok := syncEntities[op.Entity]
	if !ok {
		return rejectSync(result, "entidad no admitida"), fingerprint, false
	}
	if op.Action != "create" && op.Action != "update" && op.Action != "delete" {
		return rejectSync(result, "action debe ser create, update o delete"), fingerprint, false
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, uid+":op:"+strings.ToLower(op.OpID)); err != nil {
		return rejectSync(result, "no se pudo serializar la operación"), fingerprint, false
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, uid+":entity:"+entity.table+":"+strings.ToLower(op.EntityID)); err != nil {
		return rejectSync(result, "no se pudo serializar el registro"), fingerprint, false
	}

	var priorFingerprint string
	var priorJSON []byte
	err := tx.QueryRow(ctx,
		`SELECT fingerprint, result FROM sync_operations WHERE usuario_id = $1 AND op_id = $2`,
		uid, op.OpID).Scan(&priorFingerprint, &priorJSON)
	if err == nil {
		if priorFingerprint != fingerprint {
			return rejectSync(result, "op_id ya fue utilizado por otra operación"), fingerprint, false
		}
		if err := json.Unmarshal(priorJSON, &result); err != nil {
			return rejectSync(result, "confirmación de operación inválida"), fingerprint, false
		}
		if result.Status == "applied" || result.Status == "duplicate" {
			result.Status = "duplicate"
			result.Error = ""
		}
		return result, fingerprint, false
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return rejectSync(result, "no se pudo consultar la operación"), fingerprint, false
	}

	payload, err := syncPayload(op.Payload)
	if err != nil {
		return rejectSync(result, err.Error()), fingerprint, false
	}
	if op.Action == "delete" && len(payload) != 0 {
		return rejectSync(result, "delete requiere payload vacío"), fingerprint, false
	}
	if id, exists := payload["id"]; exists {
		if op.Action == "delete" {
			return rejectSync(result, "delete requiere payload vacío"), fingerprint, false
		}
		var payloadID string
		if json.Unmarshal(id, &payloadID) != nil || !strings.EqualFold(payloadID, op.EntityID) {
			return rejectSync(result, "payload.id debe coincidir con entity_id"), fingerprint, false
		}
		delete(payload, "id")
	}
	for key := range payload {
		if !containsString(entity.fields, key) {
			return rejectSync(result, "payload contiene un campo no admitido"), fingerprint, false
		}
	}
	if op.Action != "delete" && len(payload) == 0 {
		return rejectSync(result, "payload no puede estar vacío"), fingerprint, false
	}
	if op.Entity == "scheduled_expense_payments" && op.Action != "delete" {
		programadoID, exists := payload["programado_id"]
		if (op.Action == "create" && !exists) || (exists && bytes.Equal(bytes.TrimSpace(programadoID), []byte("null"))) {
			return rejectSync(result, "programado_id es obligatorio para un pago programado"), fingerprint, false
		}
	}
	if (op.Entity == "expenses" || op.Entity == "scheduled_expense_payments") && op.Action == "create" {
		programadoID, linked := payload["programado_id"]
		programadoFecha, hasDueDate := payload["programado_fecha"]
		if linked && !bytes.Equal(bytes.TrimSpace(programadoID), []byte("null")) &&
			(!hasDueDate || bytes.Equal(bytes.TrimSpace(programadoFecha), []byte("null"))) {
			return rejectSync(result, "programado_fecha es obligatoria para un pago programado"), fingerprint, false
		}
		if (!linked || bytes.Equal(bytes.TrimSpace(programadoID), []byte("null"))) &&
			hasDueDate && !bytes.Equal(bytes.TrimSpace(programadoFecha), []byte("null")) {
			return rejectSync(result, "programado_fecha requiere programado_id"), fingerprint, false
		}
	}
	if op.Entity == "incomes" && op.Action == "create" {
		programadoID, linked := payload["ingreso_programado_id"]
		programadoFecha, hasDueDate := payload["programado_fecha"]
		if linked && !bytes.Equal(bytes.TrimSpace(programadoID), []byte("null")) &&
			(!hasDueDate || bytes.Equal(bytes.TrimSpace(programadoFecha), []byte("null"))) {
			return rejectSync(result, "programado_fecha es obligatoria para un ingreso programado"), fingerprint, false
		}
		if (!linked || bytes.Equal(bytes.TrimSpace(programadoID), []byte("null"))) &&
			hasDueDate && !bytes.Equal(bytes.TrimSpace(programadoFecha), []byte("null")) {
			return rejectSync(result, "programado_fecha requiere ingreso_programado_id"), fingerprint, false
		}
	}
	if op.Action == "update" && op.Entity == "expenses" &&
		(payload["programado_id"] != nil || payload["programado_fecha"] != nil) {
		return rejectSync(result, "la referencia al vencimiento no se modifica al editar el pago"), fingerprint, false
	}
	if op.Action == "update" && op.Entity == "scheduled_expense_payments" &&
		(payload["programado_id"] != nil || payload["programado_fecha"] != nil) {
		return rejectSync(result, "la referencia al vencimiento no se modifica al editar el pago"), fingerprint, false
	}
	if op.Action == "update" && op.Entity == "incomes" &&
		(payload["ingreso_programado_id"] != nil || payload["programado_fecha"] != nil) {
		return rejectSync(result, "la referencia al ingreso programado no se modifica al editar el ingreso"), fingerprint, false
	}

	var current []byte
	where := "id = $1 AND usuario_id = $2"
	if entity.filter != "" {
		where += " AND " + entity.filter
	}
	currentQuery := fmt.Sprintf(`SELECT (to_jsonb(t) - 'usuario_id' - 'creado_en')::text FROM %s t WHERE %s FOR UPDATE`, entity.table, qualifySyncWhere(where))
	err = tx.QueryRow(ctx, currentQuery, op.EntityID, uid).Scan(&current)
	exists := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return rejectSync(result, "no se pudo leer el registro"), fingerprint, false
	}
	if !exists && op.Action != "create" {
		if op.Action == "delete" {
			var deleted bool
			err = tx.QueryRow(ctx, `
				SELECT EXISTS (SELECT 1 FROM sync_tombstones
				WHERE usuario_id = $1 AND entidad = $2 AND entidad_id = $3)`,
				uid, op.Entity, op.EntityID).Scan(&deleted)
			if err == nil && deleted {
				result.Status = "duplicate"
				return result, fingerprint, true
			}
		}
		return rejectSync(result, "registro inexistente"), fingerprint, false
	}
	if exists && op.Action == "create" {
		matches, compareErr := syncPayloadMatches(payload, current)
		if compareErr == nil && matches {
			result.Status = "duplicate"
			return result, fingerprint, true
		}
		return conflictSync(result), fingerprint, false
	}
	if !exists && op.Action == "create" {
		// If the UUID belongs to another user, don't reveal that fact or overwrite it.
		var globallyExists bool
		err = tx.QueryRow(ctx, fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s WHERE id = $1)`, entity.table), op.EntityID).Scan(&globallyExists)
		if err != nil {
			return rejectSync(result, "no se pudo validar el identificador"), fingerprint, false
		}
		if globallyExists {
			return rejectSync(result, "entity_id no disponible"), fingerprint, false
		}
	}

	if op.Action == "update" {
		base, err := syncCanonicalRecord(op.BaseRecord)
		if err != nil {
			return rejectSync(result, "update requiere base_record"), fingerprint, false
		}
		currentCanonical, err := syncCanonicalRecord(current)
		if err != nil {
			return rejectSync(result, "registro actual inválido"), fingerprint, false
		}
		if !reflect.DeepEqual(base, currentCanonical) {
			return conflictSync(result), fingerprint, false
		}
	}
	if err := validateSyncReferences(ctx, tx, uid, entity.refs, payload); err != nil {
		return rejectSync(result, err.Error()), fingerprint, false
	}

	switch op.Action {
	case "create":
		err = insertSyncRecord(ctx, tx, entity, uid, op.EntityID, op.Payload, payload)
		if err == nil && entity.table == "gastos" && hasNonNullJSONField(payload, "programado_id") {
			var advanced bool
			advanced, err = advanceScheduledExpense(ctx, tx, uid, payload)
			if err == nil && !advanced {
				return conflictSync(result), fingerprint, false
			}
		}
		if err == nil && entity.table == "ingresos" && hasNonNullJSONField(payload, "ingreso_programado_id") {
			var received bool
			received, err = a.applyScheduledIncomeReceipt(ctx, tx, uid, op.EntityID, payload)
			if err == nil && !received {
				return conflictSync(result), fingerprint, false
			}
		}
	case "update":
		err = updateSyncRecord(ctx, tx, entity, uid, op.EntityID, op.Payload, payload)
	case "delete":
		_, err = tx.Exec(ctx, fmt.Sprintf(`DELETE FROM %s t WHERE %s`, entity.table, qualifySyncWhere(where)), op.EntityID, uid)
	}
	if err != nil {
		if isSyncConstraintError(err) {
			return rejectSync(result, "datos inválidos o duplicados"), fingerprint, false
		}
		return rejectSync(result, "no se pudo aplicar la operación"), fingerprint, false
	}
	result.Status = "applied"
	return result, fingerprint, true
}

func advanceScheduledExpense(ctx context.Context, tx pgx.Tx, uid string, payload map[string]json.RawMessage) (bool, error) {
	var programadoID, scheduledDate string
	if json.Unmarshal(payload["programado_id"], &programadoID) != nil ||
		json.Unmarshal(payload["programado_fecha"], &scheduledDate) != nil {
		return false, errors.New("referencia de vencimiento inválida")
	}
	expectedDate, err := time.Parse(dateLayout, scheduledDate)
	if err != nil {
		return false, errors.New("programado_fecha debe tener formato AAAA-MM-DD")
	}
	var actualDate time.Time
	var frequency string
	var dueDay int
	var active bool
	err = tx.QueryRow(ctx, `
		SELECT fecha_proximo_pago, frecuencia, dia_vencimiento::int, activo
		FROM gastos_programados
		WHERE id = $1 AND usuario_id = $2
		FOR UPDATE`, programadoID, uid).Scan(&actualDate, &frequency, &dueDay, &active)
	if err != nil {
		return false, err
	}
	if !active || !actualDate.Equal(expectedDate) {
		return false, nil
	}
	if frequency == "unico" {
		_, err = tx.Exec(ctx, `UPDATE gastos_programados SET activo = false WHERE id = $1 AND usuario_id = $2`, programadoID, uid)
		return err == nil, err
	}
	nextDate, err := siguienteFechaProgramada(expectedDate, frequency, dueDay)
	if err != nil {
		return false, err
	}
	_, err = tx.Exec(ctx, `UPDATE gastos_programados SET fecha_proximo_pago = $3 WHERE id = $1 AND usuario_id = $2`, programadoID, uid, nextDate)
	return err == nil, err
}

func hasNonNullJSONField(values map[string]json.RawMessage, key string) bool {
	raw, exists := values[key]
	return exists && !bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func (a *API) applyScheduledIncomeReceipt(ctx context.Context, tx pgx.Tx, uid, incomeID string, payload map[string]json.RawMessage) (bool, error) {
	var scheduledID, scheduledDate, receivedDate string
	var amount float64
	if json.Unmarshal(payload["ingreso_programado_id"], &scheduledID) != nil ||
		json.Unmarshal(payload["programado_fecha"], &scheduledDate) != nil ||
		json.Unmarshal(payload["fecha"], &receivedDate) != nil ||
		json.Unmarshal(payload["monto"], &amount) != nil {
		return false, errors.New("referencia de ingreso programado inválida")
	}
	expectedDate, err := time.Parse(dateLayout, scheduledDate)
	if err != nil {
		return false, errors.New("programado_fecha debe tener formato AAAA-MM-DD")
	}
	receivedOn, err := time.Parse(dateLayout, receivedDate)
	if err != nil {
		return false, errors.New("fecha debe tener formato AAAA-MM-DD")
	}
	if receivedOn.After(a.hoy()) {
		return false, nil
	}

	var actualDate time.Time
	var frequency string
	var dueDay int
	var active bool
	err = tx.QueryRow(ctx, `
		SELECT fecha_proximo_ingreso, frecuencia, dia_ingreso::int, activo
		FROM ingresos_programados
		WHERE id = $1 AND usuario_id = $2
		FOR UPDATE`, scheduledID, uid).Scan(&actualDate, &frequency, &dueDay, &active)
	if err != nil {
		return false, err
	}
	if !active || !actualDate.Equal(expectedDate) {
		return false, nil
	}
	var assigned float64
	err = tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(monto), 0)::float8
		FROM asignaciones_ingresos_programados
		WHERE ingreso_programado_id = $1 AND fecha_ingreso_programada = $2`,
		scheduledID, expectedDate).Scan(&assigned)
	if err != nil {
		return false, err
	}
	if assigned > amount+0.005 {
		return false, nil
	}
	if _, err := tx.Exec(ctx, `UPDATE ingresos SET periodicidad = $3 WHERE id = $1 AND usuario_id = $2`,
		incomeID, uid, periodicidadIngresoRecibido(frequency)); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO programado_apartados
		    (programado_id, usuario_id, fecha_programada, monto, fecha, ingreso_programado_id, ingreso_fecha_programada)
		SELECT x.gasto_programado_id, x.usuario_id, x.fecha_gasto_programada, x.monto, $3,
		       x.ingreso_programado_id, x.fecha_ingreso_programada
		FROM asignaciones_ingresos_programados x
		JOIN gastos_programados g ON g.id = x.gasto_programado_id AND g.usuario_id = x.usuario_id
		WHERE x.ingreso_programado_id = $1 AND x.fecha_ingreso_programada = $2
		  AND g.activo AND g.fecha_proximo_pago = x.fecha_gasto_programada`,
		scheduledID, expectedDate, receivedOn); err != nil {
		return false, err
	}
	if frequency == "unico" {
		_, err = tx.Exec(ctx, `UPDATE ingresos_programados SET activo = false WHERE id = $1 AND usuario_id = $2`, scheduledID, uid)
	} else {
		nextDate, nextErr := siguienteFechaIngreso(expectedDate, frequency, dueDay)
		if nextErr != nil {
			return false, nextErr
		}
		_, err = tx.Exec(ctx, `UPDATE ingresos_programados SET fecha_proximo_ingreso = $3 WHERE id = $1 AND usuario_id = $2`, scheduledID, uid, nextDate)
	}
	return err == nil, err
}

func insertSyncRecord(ctx context.Context, tx pgx.Tx, entity syncEntity, uid, id string, raw json.RawMessage, payload map[string]json.RawMessage) error {
	fields := make([]string, 0, len(payload))
	for field := range payload {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	columns := []string{"id", "usuario_id"}
	values := []string{"$2::uuid", "$1::uuid"}
	for _, field := range fields {
		columns = append(columns, field)
		values = append(values, fmt.Sprintf("(jsonb_populate_record(NULL::%s, $3::jsonb)).%s", entity.table, field))
	}
	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", entity.table, strings.Join(columns, ","), strings.Join(values, ","))
	_, err := tx.Exec(ctx, query, uid, id, raw)
	return err
}

func updateSyncRecord(ctx context.Context, tx pgx.Tx, entity syncEntity, uid, id string, raw json.RawMessage, payload map[string]json.RawMessage) error {
	fields := make([]string, 0, len(payload))
	for field := range payload {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	assignments := make([]string, 0, len(fields))
	for _, field := range fields {
		assignments = append(assignments, fmt.Sprintf("%s = (jsonb_populate_record(NULL::%s, $3::jsonb)).%s", field, entity.table, field))
	}
	query := fmt.Sprintf("UPDATE %s t SET %s WHERE t.id = $1 AND t.usuario_id = $2", entity.table, strings.Join(assignments, ","))
	_, err := tx.Exec(ctx, query, id, uid, raw)
	return err
}

func syncPayload(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, errors.New("payload debe ser un objeto JSON")
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil || payload == nil {
		return nil, errors.New("payload debe ser un objeto JSON")
	}
	return payload, nil
}

func syncCanonicalRecord(raw json.RawMessage) (map[string]any, error) {
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return nil, errors.New("record is not an object")
	}
	delete(value, "usuario_id")
	delete(value, "creado_en")
	delete(value, "saldo_actual")
	return value, nil
}

func syncPayloadMatches(payload map[string]json.RawMessage, current json.RawMessage) (bool, error) {
	var record map[string]json.RawMessage
	if err := json.Unmarshal(current, &record); err != nil {
		return false, err
	}
	for field, expected := range payload {
		var left, right any
		if err := json.Unmarshal(expected, &left); err != nil {
			return false, err
		}
		if err := json.Unmarshal(record[field], &right); err != nil || !reflect.DeepEqual(left, right) {
			return false, err
		}
	}
	return true, nil
}

func validateSyncReferences(ctx context.Context, tx pgx.Tx, uid string, refs map[string]string, payload map[string]json.RawMessage) error {
	for field, table := range refs {
		raw, exists := payload[field]
		if !exists || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			continue
		}
		var id string
		if json.Unmarshal(raw, &id) != nil || !syncUUIDPattern.MatchString(id) {
			return fmt.Errorf("%s debe ser UUID o null", field)
		}
		var belongs bool
		err := tx.QueryRow(ctx, fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s WHERE id = $1 AND usuario_id = $2)`, table), id, uid).Scan(&belongs)
		if err != nil || !belongs {
			return fmt.Errorf("%s no pertenece a la cuenta autenticada", field)
		}
	}
	return nil
}

func syncFingerprint(op syncOperation) string {
	value, _ := json.Marshal(struct {
		Entity   string `json:"entity"`
		Action   string `json:"action"`
		EntityID string `json:"entity_id"`
		Payload  any    `json:"payload"`
		Base     any    `json:"base_record"`
	}{op.Entity, op.Action, op.EntityID, canonicalSyncJSON(op.Payload), canonicalSyncJSON(op.BaseRecord)})
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func canonicalSyncJSON(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return string(raw)
	}
	return value
}

func mustJSON(value any) []byte {
	data, _ := json.Marshal(value)
	return data
}

func rejectSync(result syncResult, message string) syncResult {
	result.Status = "rejected"
	result.Error = message
	return result
}

func conflictSync(result syncResult) syncResult {
	result.Status = "conflict"
	result.Error = "el registro cambió en el servidor; recupere el snapshot antes de reintentar"
	return result
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func isSyncConstraintError(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && (strings.HasPrefix(pgErr.Code, "22") || strings.HasPrefix(pgErr.Code, "23"))
}
