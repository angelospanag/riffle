package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/angelospanag/riffle/internal/config"
	"github.com/angelospanag/riffle/internal/db"
	"github.com/angelospanag/riffle/internal/feed"
	apimiddleware "github.com/angelospanag/riffle/internal/middleware"
	"github.com/angelospanag/riffle/internal/post"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/danielgtaylor/huma/v2/humacli"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

// newRouter builds the chi router and Huma API, wiring feed and post routes
// against the given queries implementation.
func newRouter(queries db.Querier, logger *slog.Logger) (*chi.Mux, huma.API) {
	router := chi.NewMux()

	api := humachi.New(router, huma.DefaultConfig("Riffle API", "1.0.0"))
	api.UseMiddleware(apimiddleware.Logging(logger))

	feed.RegisterRoutes(api, queries, logger)
	post.RegisterRoutes(api, queries, logger)

	return router, api
}

func main() {
	cli := humacli.New(func(hooks humacli.Hooks, _ *struct{}) {
		// Load .env if present; silently ignored in production where env vars are injected directly.
		_ = godotenv.Load()

		logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}))
		slog.SetDefault(logger)

		cfg, err := config.Load()
		if err != nil {
			logger.Error("config error", "error", err)
			os.Exit(1)
		}

		// pgxpool.New only parses the config; it doesn't dial the database, so this
		// stays cheap even for commands (like `openapi`) that never start the server.
		pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
		if err != nil {
			logger.Error("unable to create connection pool", "error", err)
			os.Exit(1)
		}

		queries := db.New(pool)

		router, _ := newRouter(queries, logger)

		srv := &http.Server{
			Addr:         fmt.Sprintf(":%d", cfg.Port),
			Handler:      router,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
			IdleTimeout:  60 * time.Second,
		}

		hooks.OnStart(func() {
			if err := pool.Ping(context.Background()); err != nil {
				logger.Error("unable to ping database", "error", err)
				os.Exit(1)
			}
			logger.Info("database connection established")

			logger.Info("starting server", "port", cfg.Port)
			logger.Info(
				"API documentation available at",
				"url",
				fmt.Sprintf("http://localhost:%d/docs", cfg.Port),
			)
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Error("server error", "error", err)
			}
		})

		hooks.OnStop(func() {
			defer pool.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := srv.Shutdown(ctx); err != nil {
				logger.Error("server forced to shutdown", "error", err)
			}
			logger.Info("server exited")
		})
	})

	// `openapi` prints the generated spec to stdout without starting the server
	// or touching the database — the codegen/CI source for api/openapi.yaml.
	cli.Root().AddCommand(&cobra.Command{
		Use:   "openapi",
		Short: "Print the OpenAPI spec (YAML) to stdout",
		Run: func(_ *cobra.Command, _ []string) {
			logger := slog.New(
				slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}),
			)
			_, humaAPI := newRouter(nil, logger)
			b, err := humaAPI.OpenAPI().YAML()
			if err != nil {
				fmt.Fprintf(os.Stderr, "schema error: %v\n", err)
				os.Exit(1)
			}
			// Print (not Println): YAML() already ends with a newline; an extra
			// one adds a trailing blank line that would defeat a CI drift check.
			fmt.Print(string(b))
		},
	})

	cli.Run()
}
