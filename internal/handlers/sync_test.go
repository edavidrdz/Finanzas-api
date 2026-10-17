package handlers

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"finanzas-api/internal/auth"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSyncSnapshotCollectionContract(t *testing.T) {
	want := []string{
		"accounts", "balance_adjustments", "categories", "expense_drafts", "expenses", "incomes", "recurring_services",
		"scheduled_expense_allocations", "scheduled_expense_payments", "scheduled_expenses",
		"scheduled_income_assignments", "scheduled_incomes", "transfers",
	}
	got := make([]string, 0, len(syncEntities))
	for name := range syncEntities {
		got = append(got, name)
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot collections = %v, want %v", got, want)
	}
}

func TestSyncCanonicalRecordIgnoresDerivedAndServerFields(t *testing.T) {
	got, err := syncCanonicalRecord(json.RawMessage(`{"id":"x","nombre":"A","usuario_id":"private","creado_en":"now","saldo_actual":10}`))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"id": "x", "nombre": "A"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("canonical record = %#v, want %#v", got, want)
	}
}

func TestSyncFingerprintIgnoresJSONFieldOrder(t *testing.T) {
	first := syncOperation{
		OpID: "0de9810b-3c36-4d8f-bf1e-b4ae609a5d19", Entity: "categories", Action: "create",
		EntityID: "fef7f15a-0aeb-41fd-bc1d-ff6d82e1dc19",
		Payload:  json.RawMessage(`{"nombre":"Casa","tipo":"gasto"}`),
	}
	retry := first
	retry.Payload = json.RawMessage(`{"tipo":"gasto","nombre":"Casa"}`)
	if syncFingerprint(first) != syncFingerprint(retry) {
		t.Fatal("equivalent JSON payloads produced different operation fingerprints")
	}
}

func TestSyncPushIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set TEST_DATABASE_URL to run isolated PostgreSQL sync integration tests")
	}

	ctx := context.Background()
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()

	schema := "sync_test_" + randomHex(t, 8)
	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(ctx, `DROP SCHEMA `+schema+` CASCADE`)

	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	poolConfig.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, `SET search_path TO `+schema)
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	for _, name := range []string{
		"001_init.sql", "002_cuentas_y_transferencias.sql", "003_gastos_programados.sql",
		"004_ingresos_programados_y_flujo.sql", "005_google_auth.sql", "006_password_reset.sql", "007_offline_sync.sql",
		"008_gastos_pendientes_y_ubicacion.sql",
	} {
		migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(migration)); err != nil {
			t.Fatalf("migration %s: %v", name, err)
		}
	}

	userID := randomUUID(t)
	if _, err := pool.Exec(ctx,
		`INSERT INTO usuarios (id, email, nombre, password_hash) VALUES ($1, $2, 'Sync test', 'hash')`,
		userID, userID+"@example.test"); err != nil {
		t.Fatal(err)
	}
	otherUserID, otherCategoryID := randomUUID(t), randomUUID(t)
	if _, err := pool.Exec(ctx,
		`INSERT INTO usuarios (id, email, nombre, password_hash) VALUES ($1, $2, 'Other user', 'hash')`,
		otherUserID, otherUserID+"@example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO categorias (id, usuario_id, nombre, tipo) VALUES ($1, $2, 'Private', 'gasto')`,
		otherCategoryID, otherUserID); err != nil {
		t.Fatal(err)
	}
	const secret = "sync-integration-test-secret"
	token, err := auth.NewToken(secret, userID)
	if err != nil {
		t.Fatal(err)
	}
	api := &API{DB: pool, Loc: time.UTC}
	router := chi.NewRouter()
	router.Group(func(r chi.Router) {
		r.Use(auth.Middleware(secret))
		r.Get("/snapshot", api.GetSyncSnapshot)
		r.Post("/push", api.PushSync)
	})

	categoryID, opID := randomUUID(t), randomUUID(t)
	create := map[string]any{
		"operations": []any{map[string]any{
			"op_id": opID, "entity": "categories", "action": "create", "entity_id": categoryID,
			"payload": map[string]any{"nombre": "Local", "tipo": "gasto"},
		}},
	}
	first := callSyncPush(t, router, token, create)
	if first[0].Status != "applied" {
		t.Fatalf("first create status = %q (%s)", first[0].Status, first[0].Error)
	}
	retry := callSyncPush(t, router, token, create)
	if retry[0].Status != "duplicate" {
		t.Fatalf("retry status = %q, want duplicate", retry[0].Status)
	}

	duplicate := map[string]any{
		"operations": []any{map[string]any{
			"op_id": randomUUID(t), "entity": "categories", "action": "create", "entity_id": categoryID,
			"payload": map[string]any{"nombre": "Local", "tipo": "gasto"},
		}},
	}
	if got := callSyncPush(t, router, token, duplicate)[0].Status; got != "duplicate" {
		t.Fatalf("same entity UUID create status = %q, want duplicate", got)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM categorias WHERE usuario_id = $1 AND id = $2`, userID, categoryID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("category row count = %d, want 1", count)
	}
	crossUserExpense := map[string]any{"operations": []any{map[string]any{
		"op_id": randomUUID(t), "entity": "expenses", "action": "create", "entity_id": randomUUID(t),
		"payload": map[string]any{"categoria_id": otherCategoryID, "descripcion": "Invalid", "monto": 1, "fecha": "2026-10-16"},
	}}}
	if got := callSyncPush(t, router, token, crossUserExpense)[0].Status; got != "rejected" {
		t.Fatalf("cross-user reference status = %q, want rejected", got)
	}

	base := map[string]any{"id": categoryID, "nombre": "Local", "tipo": "gasto"}
	update := map[string]any{"operations": []any{map[string]any{
		"op_id": randomUUID(t), "entity": "categories", "action": "update", "entity_id": categoryID,
		"payload": map[string]any{"nombre": "Updated"}, "base_record": base,
	}}}
	if got := callSyncPush(t, router, token, update)[0].Status; got != "applied" {
		t.Fatalf("update status = %q, want applied", got)
	}
	staleUpdate := map[string]any{"operations": []any{map[string]any{
		"op_id": randomUUID(t), "entity": "categories", "action": "update", "entity_id": categoryID,
		"payload": map[string]any{"nombre": "Stale"}, "base_record": base,
	}}}
	if got := callSyncPush(t, router, token, staleUpdate)[0].Status; got != "conflict" {
		t.Fatalf("stale update status = %q, want conflict", got)
	}

	scheduledExpenseID := randomUUID(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO gastos_programados (id, usuario_id, categoria_id, nombre, monto, fecha_proximo_pago, dia_vencimiento, frecuencia)
		VALUES ($1, $2, $3, 'Internet', 100, '2026-11-01', 1, 'mensual')`,
		scheduledExpenseID, userID, categoryID); err != nil {
		t.Fatal(err)
	}
	payment := map[string]any{"operations": []any{map[string]any{
		"op_id": randomUUID(t), "entity": "scheduled_expense_payments", "action": "create",
		"entity_id": randomUUID(t), "payload": map[string]any{
			"categoria_id": categoryID, "descripcion": "Internet", "monto": 100, "fecha": "2026-11-01",
			"programado_id": scheduledExpenseID, "programado_fecha": "2026-11-01",
		},
	}}}
	if got := callSyncPush(t, router, token, payment)[0].Status; got != "applied" {
		t.Fatalf("scheduled payment status = %q, want applied", got)
	}
	var nextDue string
	if err := pool.QueryRow(ctx, `SELECT to_char(fecha_proximo_pago, 'YYYY-MM-DD') FROM gastos_programados WHERE id = $1`, scheduledExpenseID).Scan(&nextDue); err != nil {
		t.Fatal(err)
	}
	if nextDue != "2026-12-01" {
		t.Fatalf("next scheduled expense date = %s, want 2026-12-01", nextDue)
	}

	scheduledIncomeID := randomUUID(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO ingresos_programados (id, usuario_id, nombre, monto, fecha_proximo_ingreso, dia_ingreso, frecuencia)
		VALUES ($1, $2, 'Nómina', 500, '2026-11-15', 15, 'mensual')`,
		scheduledIncomeID, userID); err != nil {
		t.Fatal(err)
	}
	receipt := map[string]any{"operations": []any{map[string]any{
		"op_id": randomUUID(t), "entity": "incomes", "action": "create", "entity_id": randomUUID(t),
		"payload": map[string]any{
			"descripcion": "Nómina", "monto": 500, "fecha": time.Now().UTC().Format(dateLayout),
			"ingreso_programado_id": scheduledIncomeID, "programado_fecha": "2026-11-15",
		},
	}}}
	if got := callSyncPush(t, router, token, receipt)[0].Status; got != "applied" {
		t.Fatalf("scheduled income receipt status = %q, want applied", got)
	}
	var nextIncome string
	if err := pool.QueryRow(ctx, `SELECT to_char(fecha_proximo_ingreso, 'YYYY-MM-DD') FROM ingresos_programados WHERE id = $1`, scheduledIncomeID).Scan(&nextIncome); err != nil {
		t.Fatal(err)
	}
	if nextIncome != "2026-12-15" {
		t.Fatalf("next scheduled income date = %s, want 2026-12-15", nextIncome)
	}

	snapshotRequest := httptest.NewRequest(http.MethodGet, "/snapshot", nil)
	snapshotRequest.Header.Set("Authorization", "Bearer "+token)
	snapshotResponse := httptest.NewRecorder()
	router.ServeHTTP(snapshotResponse, snapshotRequest)
	if snapshotResponse.Code != http.StatusOK {
		t.Fatalf("snapshot status = %d, body: %s", snapshotResponse.Code, snapshotResponse.Body.String())
	}
	var snapshot map[string]json.RawMessage
	if err := json.Unmarshal(snapshotResponse.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"accounts", "balance_adjustments", "categories", "expenses", "incomes", "recurring_services",
		"scheduled_expense_allocations", "scheduled_expense_payments", "scheduled_expenses",
		"scheduled_income_assignments", "scheduled_incomes", "transfers", "remote_deletions",
	} {
		if _, exists := snapshot[name]; !exists {
			t.Errorf("snapshot missing collection %q", name)
		}
	}
	if bytes.Contains(snapshotResponse.Body.Bytes(), []byte("password_hash")) ||
		bytes.Contains(snapshotResponse.Body.Bytes(), []byte(userID)) ||
		bytes.Contains(snapshotResponse.Body.Bytes(), []byte(otherCategoryID)) {
		t.Fatal("snapshot exposed authentication data or another user's record")
	}

	remove := map[string]any{"operations": []any{map[string]any{
		"op_id": randomUUID(t), "entity": "categories", "action": "delete", "entity_id": categoryID,
		"payload": map[string]any{},
	}}}
	if got := callSyncPush(t, router, token, remove)[0].Status; got != "applied" {
		t.Fatalf("delete status = %q, want applied", got)
	}
	snapshotResponse = httptest.NewRecorder()
	router.ServeHTTP(snapshotResponse, snapshotRequest)
	if !strings.Contains(snapshotResponse.Body.String(), `"entity":"categories"`) ||
		!strings.Contains(snapshotResponse.Body.String(), categoryID) {
		t.Fatalf("remote deletion not present in snapshot: %s", snapshotResponse.Body.String())
	}
}

func callSyncPush(t *testing.T, router http.Handler, token string, body any) []syncResult {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/push", bytes.NewReader(data))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("push status = %d, body: %s", response.Code, response.Body.String())
	}
	var result struct {
		Results []syncResult `json:"results"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Results) != 1 {
		t.Fatalf("got %d results, want 1", len(result.Results))
	}
	return result.Results
}

func randomUUID(t *testing.T) string {
	t.Helper()
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	data[6] = data[6]&0x0f | 0x40
	data[8] = data[8]&0x3f | 0x80
	encoded := hex.EncodeToString(data)
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:]
}

func randomHex(t *testing.T, size int) string {
	t.Helper()
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(data)
}
