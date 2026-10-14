package handlers

import (
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"google.golang.org/api/idtoken"

	"finanzas-api/internal/auth"
	"finanzas-api/internal/httpx"
)

type googleAuthInput struct {
	IDToken  string `json:"id_token"`
	ClientID string `json:"client_id"`
}

type googleIdentity struct {
	Subject string
	Email   string
	Name    string
}

func (a *API) verifyGoogleIdentity(r *http.Request, in googleAuthInput) (googleIdentity, int, string) {
	if len(a.GoogleClientIDs) == 0 {
		return googleIdentity{}, http.StatusServiceUnavailable, "inicio de sesión con Google no está configurado en el servidor"
	}
	if _, ok := a.GoogleClientIDs[in.ClientID]; !ok || in.IDToken == "" || len(in.IDToken) > 10000 {
		return googleIdentity{}, http.StatusBadRequest, "token de Google inválido"
	}
	payload, err := idtoken.Validate(r.Context(), in.IDToken, in.ClientID)
	if err != nil {
		return googleIdentity{}, http.StatusUnauthorized, "no se pudo validar la identidad de Google"
	}
	subject, _ := payload.Claims["sub"].(string)
	email, _ := payload.Claims["email"].(string)
	emailVerified, _ := payload.Claims["email_verified"].(bool)
	name, _ := payload.Claims["name"].(string)
	if subject == "" || !emailVerified {
		return googleIdentity{}, http.StatusUnauthorized, "Google debe proporcionar un correo verificado"
	}
	email = strings.ToLower(strings.TrimSpace(email))
	name = strings.TrimSpace(name)
	if email == "" || len(email) > 200 || name == "" || len(name) > 100 {
		return googleIdentity{}, http.StatusUnauthorized, "el perfil de Google no contiene datos válidos"
	}
	return googleIdentity{Subject: subject, Email: email, Name: name}, 0, ""
}

func (a *API) GoogleLogin(w http.ResponseWriter, r *http.Request) {
	var in googleAuthInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	identity, status, message := a.verifyGoogleIdentity(r, in)
	if status != 0 {
		httpx.Error(w, status, message)
		return
	}

	var user usuarioDTO
	err := a.DB.QueryRow(r.Context(), `
		SELECT u.id::text, u.nombre, u.email
		FROM auth_identities ai
		JOIN usuarios u ON u.id = ai.usuario_id
		WHERE ai.provider = 'google' AND ai.provider_subject = $1`,
		identity.Subject).Scan(&user.ID, &user.Nombre, &user.Email)
	if err == nil {
		a.respondWithToken(w, user)
		return
	}
	if err != pgx.ErrNoRows {
		httpx.DBError(w, err)
		return
	}

	var existingID string
	err = a.DB.QueryRow(r.Context(), `SELECT id::text FROM usuarios WHERE email = $1`, identity.Email).Scan(&existingID)
	if err == nil {
		httpx.Error(w, http.StatusConflict, "ya existe una cuenta con este correo; inicia sesión y vincula Google desde Ajustes")
		return
	}
	if err != pgx.ErrNoRows {
		httpx.DBError(w, err)
		return
	}

	tx, err := a.DB.Begin(r.Context())
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	defer tx.Rollback(r.Context())

	err = tx.QueryRow(r.Context(), `
		INSERT INTO usuarios (email, nombre, password_hash)
		VALUES ($1, $2, NULL)
		RETURNING id::text`, identity.Email, identity.Name).Scan(&user.ID)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	user.Email = identity.Email
	user.Nombre = identity.Name
	for _, name := range categoriasGasto {
		if _, err := tx.Exec(r.Context(),
			`INSERT INTO categorias (usuario_id, nombre, tipo) VALUES ($1, $2, 'gasto')`,
			user.ID, name); err != nil {
			httpx.DBError(w, err)
			return
		}
	}
	for _, name := range categoriasIngreso {
		if _, err := tx.Exec(r.Context(),
			`INSERT INTO categorias (usuario_id, nombre, tipo) VALUES ($1, $2, 'ingreso')`,
			user.ID, name); err != nil {
			httpx.DBError(w, err)
			return
		}
	}
	if _, err := tx.Exec(r.Context(), `
		INSERT INTO auth_identities (usuario_id, provider, provider_subject, email)
		VALUES ($1, 'google', $2, $3)`, user.ID, identity.Subject, identity.Email); err != nil {
		httpx.DBError(w, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpx.DBError(w, err)
		return
	}
	a.respondWithToken(w, user)
}

func (a *API) LinkGoogleAccount(w http.ResponseWriter, r *http.Request) {
	var in googleAuthInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	identity, status, message := a.verifyGoogleIdentity(r, in)
	if status != 0 {
		httpx.Error(w, status, message)
		return
	}
	userID := auth.UserID(r.Context())
	var existingUserID string
	err := a.DB.QueryRow(r.Context(), `
		SELECT usuario_id::text FROM auth_identities
		WHERE provider = 'google' AND provider_subject = $1`, identity.Subject).Scan(&existingUserID)
	if err == nil {
		if existingUserID == userID {
			httpx.JSON(w, http.StatusOK, map[string]bool{"linked": true})
			return
		}
		httpx.Error(w, http.StatusConflict, "esta cuenta de Google ya está vinculada a otro usuario")
		return
	}
	if err != pgx.ErrNoRows {
		httpx.DBError(w, err)
		return
	}
	_, err = a.DB.Exec(r.Context(), `
		INSERT INTO auth_identities (usuario_id, provider, provider_subject, email)
		VALUES ($1, 'google', $2, $3)`, userID, identity.Subject, identity.Email)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"linked": true})
}

func (a *API) respondWithToken(w http.ResponseWriter, user usuarioDTO) {
	token, err := auth.NewToken(a.JWTSecret, user.ID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "error interno")
		return
	}
	httpx.JSON(w, http.StatusOK, authResponse{Token: token, Usuario: user})
}
