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
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/google/uuid"

	"leadintel/backend/internal/aiclient"
	"leadintel/backend/internal/analysis"
	"leadintel/backend/internal/api"
	"leadintel/backend/internal/config"
	"leadintel/backend/internal/db"
	"leadintel/backend/internal/httpx"
	"leadintel/backend/internal/lead"
	"leadintel/backend/internal/llmsettings"
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

	ai, err := aiclient.New(cfg.AIGRPCAddr)
	if err != nil {
		return err
	}
	defer func() { _ = ai.Close() }()

	leads := lead.NewStore(pool)
	jobs := analysis.NewStore(pool)
	settings := llmsettings.NewStore(pool)

	// Jobs left running by the previous process lost their streams with it.
	// This must happen before the workers start claiming (SPEC.md §11.2).
	n, err := jobs.FailInterrupted(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		slog.Warn("marked interrupted analyses as failed", "count", n)
	}

	broker := analysis.NewBroker()
	worker := analysis.NewWorker(jobs, settings, ai, broker, analysis.WorkerConfig{
		Workers:          cfg.AnalysisWorkers,
		Timeout:          cfg.AnalysisTimeout,
		DispatchAttempts: cfg.AIDispatchAttempts,
	})
	workerCtx, stopWorkers := context.WithCancel(context.Background())
	workersDone := make(chan struct{})
	go func() {
		defer close(workersDone)
		worker.Run(workerCtx)
	}()
	defer func() {
		// Cancelling marks in-flight jobs interrupted (retryable) before exit.
		stopWorkers()
		<-workersDone
	}()

	latest := func(ctx context.Context, id uuid.UUID) (any, error) { return jobs.Latest(ctx, id) }
	analyses := analysis.NewHandler(jobs, leads, settings, worker, broker)
	handlers := server{
		Handler:         lead.NewHandler(leads, latest, analyses.CancelActive),
		analysisHandler: analyses,
		settingsHandler: llmsettings.NewHandler(settings, ai),
	}

	ready := readiness(map[string]func(context.Context) error{
		"db": pool.Ping,
		"ai": ai.Health,
	})
	srv := &http.Server{
		Addr:              cfg.APIAddr,
		Handler:           newRouter(ready, handlers, cfg.AdminToken),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	// Open event streams would otherwise hold Shutdown until its timeout;
	// clients reconnect and resume from a snapshot.
	srv.RegisterOnShutdown(broker.Close)

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
		// SPEC.md §11.2: stop accepting requests and claiming jobs, cancel
		// in-flight jobs (the worker marks them interrupted), drain up to 10s.
		slog.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		stopWorkers()
		httpErr := srv.Shutdown(shutdownCtx)
		select {
		case <-workersDone:
		case <-shutdownCtx.Done():
			slog.Warn("analysis workers did not stop within the grace period")
		}
		if httpErr != nil {
			return fmt.Errorf("http shutdown: %w", httpErr)
		}
	}
	return nil
}

const (
	shutdownGrace = 10 * time.Second
	// readyCheckTimeout bounds each dependency check of /readyz.
	readyCheckTimeout = time.Second
)

// readiness reports each dependency as "up" or "down", checked in parallel
// with a timeout each. Any dependency down makes the service unready (503).
func readiness(checks map[string]func(context.Context) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		type result struct {
			name string
			err  error
		}
		results := make(chan result, len(checks))
		for name, check := range checks {
			go func() {
				ctx, cancel := context.WithTimeout(r.Context(), readyCheckTimeout)
				defer cancel()
				results <- result{name, check(ctx)}
			}()
		}
		body := map[string]string{"status": "ok"}
		status := http.StatusOK
		for range checks {
			res := <-results
			body[res.name] = "up"
			if res.err != nil {
				body[res.name] = "down"
				body["status"] = "unavailable"
				status = http.StatusServiceUnavailable
				slog.WarnContext(r.Context(), "readiness check failed", "dependency", res.name, "err", res.err)
			}
		}
		httpx.WriteJSON(w, status, body)
	}
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

type (
	analysisHandler = *analysis.Handler
	settingsHandler = *llmsettings.Handler
)

// server composes the feature handlers into one api.ServerInterface. Each
// handler embeds api.Unimplemented; Go's embedding rules pick the shallowest
// method, so every operation resolves to the feature that implements it.
type server struct {
	*lead.Handler
	analysisHandler
	settingsHandler
}

var _ api.ServerInterface = server{}

func newRouter(ready http.HandlerFunc, handlers api.ServerInterface, adminToken string) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, httpx.RequestLogger, middleware.Recoverer)

	r.NotFound(func(w http.ResponseWriter, r *http.Request) { httpx.WriteError(w, r, httpx.ErrNotFoundRoute) })
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, r, &httpx.Error{Status: http.StatusMethodNotAllowed, Code: "method_not_allowed", Message: "Method not allowed."})
	})

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/readyz", ready)

	r.Route("/api", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			// Request/response endpoints get a hard deadline; event streams are
			// long-lived and end on their own (terminal event or disconnect).
			r.Use(timeoutExceptStreams(30 * time.Second))
			// Every /api/admin/* route requires the admin token (fails closed for new routes).
			r.Use(httpx.RequireAdmin(adminToken, "/api/admin/"))
			// Routes are generated from api/openapi.yaml (internal/api).
			api.HandlerWithOptions(handlers, api.ChiServerOptions{
				BaseRouter:       r,
				ErrorHandlerFunc: httpx.ParamError,
			})
		})
	})
	return r
}

// timeoutExceptStreams applies middleware.Timeout to every request except the
// SSE endpoints (GET …/events), whose context must outlive any fixed deadline.
func timeoutExceptStreams(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		timed := middleware.Timeout(d)(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/events") {
				next.ServeHTTP(w, r)
				return
			}
			timed.ServeHTTP(w, r)
		})
	}
}
