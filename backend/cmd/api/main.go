// Command api runs the Lead Intelligence HTTP API.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"leadintel/backend/internal/api"
	"leadintel/backend/internal/config"
	"leadintel/backend/internal/db"
	"leadintel/backend/internal/httpx"
	"leadintel/backend/internal/lead"
	"leadintel/backend/migrations"
)

func main() {
	if err := run(); err != nil {
		slog.Error("api exited", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel})))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err = pool.Ping(pingCtx)
	cancel()
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}

	if cfg.MigrateOnStart {
		if err := migrate(ctx, pool); err != nil {
			return err
		}
	}

	srv := &http.Server{
		Addr:              cfg.APIAddr,
		Handler:           newRouter(pool),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("api listening", "addr", cfg.APIAddr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http server: %w", err)
		}
	case <-ctx.Done():
		slog.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("http shutdown: %w", err)
		}
	}
	return nil
}

func migrate(ctx context.Context, pool *pgxpool.Pool) error {
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()

	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS)
	if err != nil {
		return fmt.Errorf("init migrations: %w", err)
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	for _, r := range results {
		slog.Info("migration applied", "version", r.Source.Version, "duration_ms", r.Duration.Milliseconds())
	}
	return nil
}

func newRouter(pool *pgxpool.Pool) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, httpx.RequestLogger, middleware.Recoverer)

	r.NotFound(func(w http.ResponseWriter, r *http.Request) { httpx.WriteError(w, r, httpx.ErrNotFoundRoute) })
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, r, &httpx.Error{Status: http.StatusMethodNotAllowed, Code: "method_not_allowed", Message: "Method not allowed."})
	})

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := pool.Ping(ctx); err != nil {
			httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "db": "down"})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "db": "up"})
	})

	r.Route("/api", func(r chi.Router) {
		// Request/response endpoints get a hard deadline. Long-lived streams
		// (SSE) will be mounted outside this group.
		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(30 * time.Second))
			// Routes are generated from api/openapi.yaml (internal/api).
			api.HandlerWithOptions(lead.NewHandler(lead.NewStore(pool)), api.ChiServerOptions{
				BaseRouter:       r,
				ErrorHandlerFunc: httpx.ParamError,
			})
		})
	})
	return r
}
