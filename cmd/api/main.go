package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // zonas horarias embebidas (el contenedor de Render no las trae)

	"finanzas-api/internal/auth"
	"finanzas-api/internal/config"
	"finanzas-api/internal/db"
	"finanzas-api/internal/handlers"
	"finanzas-api/internal/httpx"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/joho/godotenv"
)

func main() {
	err := godotenv.Load()

	if err != nil {
		log.Println("No se encontró .env")
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("configuración: %v", err)
	}
	loc, err := time.LoadLocation(cfg.AppTZ)
	if err != nil {
		log.Fatalf("APP_TZ inválida (%q): %v", cfg.AppTZ, err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("base de datos: %v", err)
	}
	defer pool.Close()

	api := &handlers.API{DB: pool, JWTSecret: cfg.JWTSecret, Loc: loc}

	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, middleware.Logger, middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/health/db", api.HealthDB)

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/register", api.Register)
		r.Post("/auth/login", api.Login)

		r.Group(func(r chi.Router) {
			r.Use(auth.Middleware(cfg.JWTSecret))

			r.Get("/categorias", api.ListCategorias)
			r.Post("/categorias", api.CreateCategoria)
			r.Delete("/categorias/{id}", api.DeleteCategoria)

			r.Get("/gastos", api.ListGastos)
			r.Post("/gastos", api.CreateGasto)
			r.Put("/gastos/{id}", api.UpdateGasto)
			r.Delete("/gastos/{id}", api.DeleteGasto)

			r.Get("/ingresos", api.ListIngresos)
			r.Post("/ingresos", api.CreateIngreso)
			r.Put("/ingresos/{id}", api.UpdateIngreso)
			r.Delete("/ingresos/{id}", api.DeleteIngreso)

			r.Get("/recurrentes", api.ListRecurrentes)
			r.Post("/recurrentes", api.CreateRecurrente)
			r.Post("/recurrentes/generar", api.GenerarRecurrentes)
			r.Put("/recurrentes/{id}", api.UpdateRecurrente)
			r.Delete("/recurrentes/{id}", api.DeleteRecurrente)

			r.Get("/resumen", api.GetResumen)
		})
	})

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      35 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		log.Printf("API escuchando en :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("servidor: %v", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	log.Println("servidor detenido")
}
