package handlers

import (
	"net/http"
	"strings"

	"finanzas-api/internal/auth"
	"finanzas-api/internal/httpx"
)

type authInput struct {
	Nombre   string `json:"nombre"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type usuarioDTO struct {
	ID     string `json:"id"`
	Nombre string `json:"nombre"`
	Email  string `json:"email"`
}

type authResponse struct {
	Token   string     `json:"token"`
	Usuario usuarioDTO `json:"usuario"`
}

var categoriasGasto = []string{"Luz", "Agua", "Gas", "Internet", "Teléfono", "Gasolina",
	"Alimentos", "Transporte", "Salud", "Entretenimiento", "Otros"}
var categoriasIngreso = []string{"Nómina", "Otros ingresos"}

func (a *API) Register(w http.ResponseWriter, r *http.Request) {
	var in authInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Nombre = strings.TrimSpace(in.Nombre)
	switch {
	case in.Nombre == "" || len(in.Nombre) > 100:
		httpx.Error(w, http.StatusBadRequest, "nombre es obligatorio")
		return
	case !strings.Contains(in.Email, "@") || len(in.Email) > 200:
		httpx.Error(w, http.StatusBadRequest, "email inválido")
		return
	case len(in.Password) < 8 || len(in.Password) > 72:
		httpx.Error(w, http.StatusBadRequest, "la contraseña debe tener entre 8 y 72 caracteres")
		return
	}

	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "error interno")
		return
	}

	ctx := r.Context()
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var id string
	err = tx.QueryRow(ctx,
		`INSERT INTO usuarios (email, nombre, password_hash) VALUES ($1, $2, $3) RETURNING id::text`,
		in.Email, in.Nombre, hash).Scan(&id)
	if err != nil {
		httpx.DBError(w, err) // 23505 => 409 (email ya registrado)
		return
	}
	for _, n := range categoriasGasto {
		if _, err := tx.Exec(ctx, `INSERT INTO categorias (usuario_id, nombre, tipo) VALUES ($1, $2, 'gasto')`, id, n); err != nil {
			httpx.DBError(w, err)
			return
		}
	}
	for _, n := range categoriasIngreso {
		if _, err := tx.Exec(ctx, `INSERT INTO categorias (usuario_id, nombre, tipo) VALUES ($1, $2, 'ingreso')`, id, n); err != nil {
			httpx.DBError(w, err)
			return
		}
	}
	if err := tx.Commit(ctx); err != nil {
		httpx.DBError(w, err)
		return
	}

	token, err := auth.NewToken(a.JWTSecret, id)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "error interno")
		return
	}
	httpx.JSON(w, http.StatusCreated, authResponse{Token: token, Usuario: usuarioDTO{ID: id, Nombre: in.Nombre, Email: in.Email}})
}

func (a *API) Login(w http.ResponseWriter, r *http.Request) {
	var in authInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))

	var u usuarioDTO
	var hash string
	err := a.DB.QueryRow(r.Context(),
		`SELECT id::text, nombre, email, password_hash FROM usuarios WHERE email = $1`,
		in.Email).Scan(&u.ID, &u.Nombre, &u.Email, &hash)
	// Mismo mensaje si el usuario no existe o la contraseña es incorrecta.
	if err != nil || !auth.CheckPassword(hash, in.Password) {
		httpx.Error(w, http.StatusUnauthorized, "email o contraseña incorrectos")
		return
	}
	token, err := auth.NewToken(a.JWTSecret, u.ID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "error interno")
		return
	}
	httpx.JSON(w, http.StatusOK, authResponse{Token: token, Usuario: u})
}
