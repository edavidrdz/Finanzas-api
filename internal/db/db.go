package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect abre un pool de conexiones hacia Neon (PostgreSQL).
func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("DATABASE_URL inválida: %w", err)
	}
	cfg.MaxConns = 5 // el plan gratuito de Neon tiene pocas conexiones
	cfg.MaxConnIdleTime = 5 * time.Minute
	// Compatible con el pooler de Neon (PgBouncer): evita prepared statements con nombre.
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeCacheDescribe

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	// Neon "duerme" cuando no se usa: damos margen para el primer arranque.
	pingCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("no se pudo conectar a la base de datos: %w", err)
	}
	return pool, nil
}
