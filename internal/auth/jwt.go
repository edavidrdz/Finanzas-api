package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const TokenTTL = 30 * 24 * time.Hour

func NewToken(secret, userID string) (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   userID,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(TokenTTL)),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

// ParseToken valida el token y devuelve el ID del usuario.
func ParseToken(secret, raw string) (string, error) {
	var claims jwt.RegisteredClaims
	tok, err := jwt.ParseWithClaims(raw, &claims,
		func(t *jwt.Token) (any, error) { return []byte(secret), nil },
		jwt.WithValidMethods([]string{"HS256"}),
	)
	if err != nil || !tok.Valid || claims.Subject == "" {
		return "", errors.New("token inválido")
	}
	return claims.Subject, nil
}
