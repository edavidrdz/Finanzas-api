package config

import (
	"errors"
	"os"
	"strings"
)

type Config struct {
	Port            string
	DatabaseURL     string
	JWTSecret       string
	AppTZ           string
	GoogleClientIDs []string
	CORSOrigins     []string
	PublicAPIURL    string
	SMTPHost        string
	SMTPPort        string
	SMTPUsername    string
	SMTPPassword    string
	SMTPFrom        string
}

func Load() (*Config, error) {
	c := &Config{
		Port:         os.Getenv("PORT"),
		DatabaseURL:  os.Getenv("DATABASE_URL"),
		JWTSecret:    os.Getenv("JWT_SECRET"),
		AppTZ:        os.Getenv("APP_TZ"),
		PublicAPIURL: strings.TrimRight(os.Getenv("PUBLIC_API_URL"), "/"),
		SMTPHost:     strings.TrimSpace(os.Getenv("SMTP_HOST")),
		SMTPPort:     strings.TrimSpace(os.Getenv("SMTP_PORT")),
		SMTPUsername: os.Getenv("SMTP_USERNAME"),
		SMTPPassword: os.Getenv("SMTP_PASSWORD"),
		SMTPFrom:     strings.TrimSpace(os.Getenv("SMTP_FROM")),
	}
	if c.SMTPPort == "" {
		c.SMTPPort = "587"
	}
	for _, id := range strings.Split(os.Getenv("GOOGLE_CLIENT_IDS"), ",") {
		if id = strings.TrimSpace(id); id != "" {
			c.GoogleClientIDs = append(c.GoogleClientIDs, id)
		}
	}
	for _, origin := range strings.Split(os.Getenv("CORS_ORIGINS"), ",") {
		if origin = strings.TrimSpace(origin); origin != "" {
			c.CORSOrigins = append(c.CORSOrigins, origin)
		}
	}
	if c.DatabaseURL == "" {
		return nil, errors.New("DATABASE_URL es obligatoria")
	}
	if len(c.JWTSecret) < 32 {
		return nil, errors.New("JWT_SECRET debe tener al menos 32 caracteres")
	}
	return c, nil
}
