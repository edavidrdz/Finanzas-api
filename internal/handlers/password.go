package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"finanzas-api/internal/auth"
	"finanzas-api/internal/httpx"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const passwordResetLifetime = 30 * time.Minute
const passwordResetMessage = "Si existe una cuenta con ese correo, recibirás un enlace para restablecer tu contraseña."

type passwordResetRequest struct {
	Email string `json:"email"`
}

type passwordResetInput struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

type passwordChangeInput struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

var passwordResetPage = template.Must(template.New("password-reset").Parse(`<!doctype html>
<html lang="es">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Restablecer contraseña - Mis Finanzas</title>
  <style>
    body { font: 16px system-ui, sans-serif; background: #f4f6fa; color: #172033; margin: 0; padding: 24px; }
    main { max-width: 420px; margin: 8vh auto; padding: 24px; background: white; border-radius: 16px; }
    label { display: block; margin: 16px 0 6px; font-weight: 600; }
    input, button { box-sizing: border-box; width: 100%; padding: 12px; border-radius: 8px; font: inherit; }
    input { border: 1px solid #ccd2df; }
    button { margin-top: 20px; border: 0; color: white; background: #3157c8; font-weight: 700; }
    p { line-height: 1.5; }
  </style>
</head>
<body>
  <main>
    <h1>Restablecer contraseña</h1>
    {{if .Message}}<p role="status">{{.Message}}</p>{{else}}
    <p>Elige una contraseña nueva. Debe tener entre 8 y 72 caracteres.</p>
    <form method="post" action="/api/v1/auth/password/reset">
      <input id="reset-token" type="hidden" name="token" value="{{.Token}}">
      <label for="new-password">Nueva contraseña</label>
      <input id="new-password" name="new_password" type="password" minlength="8" maxlength="72" autocomplete="new-password" required>
      <label for="confirm-password">Confirma la contraseña</label>
      <input id="confirm-password" type="password" minlength="8" maxlength="72" autocomplete="new-password" required>
      <button type="submit">Guardar contraseña</button>
    </form>
    <script>
      document.getElementById('reset-token').value = new URLSearchParams(location.hash.slice(1)).get('token') || '';
      document.querySelector('form').addEventListener('submit', function (event) {
        if (document.getElementById('new-password').value !== document.getElementById('confirm-password').value) {
          event.preventDefault();
          alert('Las contraseñas no coinciden.');
        }
      });
    </script>
    {{end}}
  </main>
</body>
</html>`))

func (a *API) RequestPasswordReset(w http.ResponseWriter, r *http.Request) {
	if !a.SMTP.Configured() || a.PublicAPIURL == "" {
		httpx.Error(w, http.StatusServiceUnavailable, "el envío de correos no está configurado")
		return
	}
	var in passwordResetRequest
	if !httpx.Decode(w, r, &in) {
		return
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	address, err := mail.ParseAddress(in.Email)
	if err != nil || address.Address != in.Email || len(in.Email) > 200 {
		httpx.Error(w, http.StatusBadRequest, "email inválido")
		return
	}

	tx, err := a.DB.Begin(r.Context())
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	var userID string
	var email string
	err = tx.QueryRow(r.Context(),
		`SELECT id::text, email FROM usuarios WHERE email = $1 FOR UPDATE`, in.Email,
	).Scan(&userID, &email)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.JSON(w, http.StatusOK, map[string]string{"message": passwordResetMessage})
		return
	}
	if err != nil {
		httpx.DBError(w, err)
		return
	}

	var recentRequests int
	err = tx.QueryRow(r.Context(), `
		SELECT count(*) FROM password_reset_tokens
		WHERE usuario_id = $1 AND creado_en > now() - interval '1 hour'`,
		userID,
	).Scan(&recentRequests)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	if recentRequests >= 3 {
		httpx.JSON(w, http.StatusOK, map[string]string{"message": passwordResetMessage})
		return
	}

	rawToken, tokenHash, err := newPasswordResetToken()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "no se pudo generar el enlace de recuperación")
		return
	}
	_, err = tx.Exec(r.Context(), `
		DELETE FROM password_reset_tokens
		WHERE usuario_id = $1 AND usado_en IS NULL`, userID)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	_, err = tx.Exec(r.Context(), `
		INSERT INTO password_reset_tokens (usuario_id, token_hash, expira_en)
		VALUES ($1, $2, $3)`,
		userID, tokenHash, time.Now().Add(passwordResetLifetime))
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpx.DBError(w, err)
		return
	}

	resetURL, err := url.Parse(a.PublicAPIURL)
	if err != nil || resetURL.Scheme != "https" || resetURL.Host == "" {
		log.Printf("URL pública de API inválida para recuperación de contraseña")
		_, _ = a.DB.Exec(r.Context(), `DELETE FROM password_reset_tokens WHERE token_hash = $1`, tokenHash)
		httpx.Error(w, http.StatusServiceUnavailable, "el envío de correos no está configurado correctamente")
		return
	}
	resetURL.Path = strings.TrimRight(resetURL.Path, "/") + "/api/v1/auth/password/reset"
	resetURL.Fragment = "token=" + url.QueryEscape(rawToken)
	body := fmt.Sprintf(
		"Recibimos una solicitud para cambiar la contraseña de tu cuenta de Mis Finanzas.\r\n\r\n"+
			"Abre este enlace para elegir una contraseña nueva (vence en 30 minutos):\r\n%s\r\n\r\n"+
			"Si no solicitaste el cambio, ignora este mensaje. Tu contraseña no cambiará.\r\n",
		resetURL.String(),
	)
	if err := a.SMTP.Send(r.Context(), email, "Recupera tu contraseña de Mis Finanzas", body); err != nil {
		log.Printf("no se pudo enviar correo de recuperación: %v", err)
		_, _ = a.DB.Exec(r.Context(), `DELETE FROM password_reset_tokens WHERE token_hash = $1`, tokenHash)
		httpx.JSON(w, http.StatusOK, map[string]string{"message": passwordResetMessage})
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"message": passwordResetMessage})
}

func (a *API) ResetPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writePasswordResetPage(w, http.StatusOK, "", "")
		return
	}

	var in passwordResetInput
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		if err := r.ParseForm(); err != nil {
			writePasswordResetPage(w, http.StatusBadRequest, "", "No se pudo leer la solicitud.")
			return
		}
		in.Token = r.FormValue("token")
		in.NewPassword = r.FormValue("new_password")
	} else if !httpx.Decode(w, r, &in) {
		return
	}
	if !validResetToken(in.Token) {
		resetPasswordError(w, r, http.StatusBadRequest, "El enlace no es válido o ya venció. Solicita uno nuevo.", "")
		return
	}
	if err := validateNewPassword(in.NewPassword); err != nil {
		resetPasswordError(w, r, http.StatusBadRequest, err.Error(), in.Token)
		return
	}
	tokenHash := hashResetToken(in.Token)
	var tokenExists bool
	if err := a.DB.QueryRow(r.Context(), `
		SELECT EXISTS (
			SELECT 1 FROM password_reset_tokens
			WHERE token_hash = $1 AND usado_en IS NULL AND expira_en > now()
		)`, tokenHash).Scan(&tokenExists); err != nil {
		httpx.DBError(w, err)
		return
	}
	if !tokenExists {
		resetPasswordError(w, r, http.StatusBadRequest, "El enlace no es válido o ya venció. Solicita uno nuevo.", "")
		return
	}
	hash, err := auth.HashPassword(in.NewPassword)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "no se pudo guardar la contraseña")
		return
	}
	tx, err := a.DB.Begin(r.Context())
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	var userID string
	err = tx.QueryRow(r.Context(), `
		UPDATE password_reset_tokens
		SET usado_en = now()
		WHERE token_hash = $1 AND usado_en IS NULL AND expira_en > now()
		RETURNING usuario_id::text`, tokenHash).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		resetPasswordError(w, r, http.StatusBadRequest, "El enlace no es válido o ya venció. Solicita uno nuevo.", "")
		return
	}
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	if _, err := tx.Exec(r.Context(),
		`UPDATE usuarios SET password_hash = $1 WHERE id = $2`, hash, userID); err != nil {
		httpx.DBError(w, err)
		return
	}
	if _, err := tx.Exec(r.Context(),
		`DELETE FROM password_reset_tokens WHERE usuario_id = $1 AND token_hash <> $2`, userID, tokenHash); err != nil {
		httpx.DBError(w, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpx.DBError(w, err)
		return
	}
	if r.Method == http.MethodPost && strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		writePasswordResetPage(w, http.StatusOK, "", "Tu contraseña se actualizó. Ya puedes volver a Mis Finanzas e iniciar sesión.")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"message": "contraseña actualizada"})
}

func (a *API) ChangePassword(w http.ResponseWriter, r *http.Request) {
	var in passwordChangeInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	if err := validateNewPassword(in.NewPassword); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	userID := auth.UserID(r.Context())
	var currentHash pgtype.Text
	if err := a.DB.QueryRow(r.Context(),
		`SELECT password_hash FROM usuarios WHERE id = $1`, userID).Scan(&currentHash); err != nil {
		httpx.DBError(w, err)
		return
	}
	if currentHash.Valid && !auth.CheckPassword(currentHash.String, in.CurrentPassword) {
		httpx.Error(w, http.StatusBadRequest, "la contraseña actual es incorrecta")
		return
	}
	if !currentHash.Valid && in.CurrentPassword != "" {
		httpx.Error(w, http.StatusBadRequest, "esta cuenta no tiene una contraseña anterior")
		return
	}
	hash, err := auth.HashPassword(in.NewPassword)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "no se pudo guardar la contraseña")
		return
	}
	tx, err := a.DB.Begin(r.Context())
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck
	if _, err := tx.Exec(r.Context(), `UPDATE usuarios SET password_hash = $1 WHERE id = $2`, hash, userID); err != nil {
		httpx.DBError(w, err)
		return
	}
	if _, err := tx.Exec(r.Context(), `DELETE FROM password_reset_tokens WHERE usuario_id = $1`, userID); err != nil {
		httpx.DBError(w, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpx.DBError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"message": "contraseña actualizada"})
}

func newPasswordResetToken() (string, string, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", "", err
	}
	token := base64.RawURLEncoding.EncodeToString(random)
	return token, hashResetToken(token), nil
}

func hashResetToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func validResetToken(token string) bool {
	if len(token) != 43 {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) != 32 {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(base64.RawURLEncoding.EncodeToString(decoded))) == 1
}

func validateNewPassword(password string) error {
	if len(password) < 8 || len(password) > 72 {
		return errors.New("la contraseña debe tener entre 8 y 72 caracteres")
	}
	return nil
}

func resetPasswordError(w http.ResponseWriter, r *http.Request, status int, message, token string) {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		writePasswordResetPage(w, status, token, message)
		return
	}
	httpx.Error(w, status, message)
}

func writePasswordResetPage(w http.ResponseWriter, status int, token, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(status)
	if err := passwordResetPage.Execute(w, struct {
		Token   string
		Message string
	}{Token: token, Message: message}); err != nil {
		log.Printf("no se pudo renderizar la página de recuperación: %v", err)
	}
}
