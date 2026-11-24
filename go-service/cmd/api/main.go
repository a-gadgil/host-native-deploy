package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
)

type Config struct {
	Port      string
	DBDSN     string
	RedisAddr string
	Version   string
}

type Item struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// App encapsulates dependencies instead of relying on package-level globals.
type App struct {
	db     *sql.DB
	rdb    *redis.Client
	logger *slog.Logger
	cfg    Config
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg := Config{
		Port:      getEnv("APP_PORT", "8080"),
		DBDSN:     getEnv("DB_DSN", "appuser:apppass@tcp(127.0.0.1:3306)/appdb?parseTime=true"),
		RedisAddr: getEnv("REDIS_ADDR", "127.0.0.1:6379"),
		Version:   getEnv("APP_VERSION", "1.0.0-dev"),
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 1. MySQL Connection Pool
	db, err := sql.Open("mysql", cfg.DBDSN)
	if err != nil {
		logger.Error("failed to open mysql driver", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		logger.Warn("mysql not reachable at startup", "error", err)
	}

	// 2. Redis Client
	rdb := redis.NewClient(&redis.Options{
		Addr:         cfg.RedisAddr,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})
	defer rdb.Close()

	app := &App{
		db:     db,
		rdb:    rdb,
		logger: logger,
		cfg:    cfg,
	}

	// 3. HTTP Router (Go 1.22+ method-aware routing)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", app.handleHealth)
	mux.HandleFunc("GET /items", app.handleListItems)

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// 4. Non-blocking Server Start + Graceful Shutdown
	go func() {
		logger.Info("starting go-service", "port", cfg.Port, "version", cfg.Version)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server exited unexpectedly", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	logger.Info("shutdown signal received, draining connections...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("forced shutdown", "error", err)
	}
	logger.Info("server stopped cleanly")
}

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	dbStatus := "up"
	if err := a.db.PingContext(ctx); err != nil {
		dbStatus = "down"
	}

	redisStatus := "up"
	if err := a.rdb.Ping(ctx).Err(); err != nil {
		redisStatus = "down"
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"version": a.cfg.Version,
		"mysql":   dbStatus,
		"redis":   redisStatus,
	})
}

func (a *App) handleListItems(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	// Check Redis cache first
	if cached, err := a.rdb.Get(ctx, "items:all").Result(); err == nil {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Cache", "HIT")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(cached))
		return
	}

	rows, err := a.db.QueryContext(ctx, "SELECT id, name FROM items LIMIT 50")
	if err != nil {
		a.logger.Error("query failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "database query failed"})
		return
	}
	defer rows.Close()

	items := make([]Item, 0)
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.Name); err != nil {
			a.logger.Error("row scan failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read rows"})
			return
		}
		items = append(items, it)
	}

	if payload, err := json.Marshal(items); err == nil {
		_ = a.rdb.Set(ctx, "items:all", payload, 30*time.Second).Err()
	}

	w.Header().Set("X-Cache", "MISS")
	writeJSON(w, http.StatusOK, items)
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func getEnv(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return fallback
}
