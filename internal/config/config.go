package config

import (
	"errors"
	"os"
)

type Config struct {
	Port        string
	DatabaseURL string
	JWTSecret   string
	AppTZ       string
}

func Load() (*Config, error) {
	c := &Config{
		Port:        os.Getenv("PORT"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		JWTSecret:   os.Getenv("JWT_SECRET"),
		AppTZ:       os.Getenv("APP_TZ"),
	}
	if c.DatabaseURL == "" {
		return nil, errors.New("DATABASE_URL es obligatoria")
	}
	if len(c.JWTSecret) < 32 {
		return nil, errors.New("JWT_SECRET debe tener al menos 32 caracteres")
	}
	return c, nil
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
