package httpx

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func Error(w http.ResponseWriter, status int, msg string) {
	JSON(w, status, map[string]string{"error": msg})
}

// Decode lee el JSON del body (máx. 1 MB) y rechaza campos desconocidos.
func Decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		Error(w, http.StatusBadRequest, "JSON inválido: "+err.Error())
		return false
	}
	return true
}

// DBError traduce errores de PostgreSQL a respuestas HTTP sin filtrar detalles internos.
func DBError(w http.ResponseWriter, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		Error(w, http.StatusNotFound, "no encontrado")
		return
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			Error(w, http.StatusConflict, "ya existe un registro igual")
			return
		case "22P02", "22007", "22008", "23503", "23514": // formato/FK/check inválidos
			Error(w, http.StatusBadRequest, "datos inválidos")
			return
		}
	}
	log.Printf("error de base de datos: %v", err)
	Error(w, http.StatusInternalServerError, "error interno")
}
