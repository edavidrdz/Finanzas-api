package auth

import (
	"context"
	"net/http"
	"strings"

	"finanzas-api/internal/httpx"
)

type ctxKey struct{}

// Middleware exige "Authorization: Bearer <token>" y guarda el ID de usuario en el contexto.
func Middleware(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok || raw == "" {
				httpx.Error(w, http.StatusUnauthorized, "falta el token de autenticación")
				return
			}
			uid, err := ParseToken(secret, raw)
			if err != nil {
				httpx.Error(w, http.StatusUnauthorized, "token inválido o expirado")
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, uid)))
		})
	}
}

func UserID(ctx context.Context) string {
	v, _ := ctx.Value(ctxKey{}).(string)
	return v
}
