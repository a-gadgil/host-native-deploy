package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aditya/polyglot-rust-mariadb/go-api/internal/config"
	"github.com/aditya/polyglot-rust-mariadb/go-api/internal/handler"
	"github.com/aditya/polyglot-rust-mariadb/go-api/internal/store"
)

func main() {
	healthcheck := flag.Bool("healthcheck", false, "run HTTP healthcheck against local /healthz")
	flag.Parse()

	cfg := config.Load()

	if *healthcheck {
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Get("http://127.0.0.1:" + cfg.AppPort + "/healthz")
		if err != nil || resp.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		os.Exit(0)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	st, err := store.New(ctx, cfg.MariaDBDSN, cfg.RedisAddr)
	if err != nil {
		log.Fatalf("failed to initialize data stores: %v", err)
	}
	defer st.Close()

	mux := http.NewServeMux()
	h := handler.New(st, cfg.RustAPIURL, cfg.AppEnv)
	h.Register(mux)

	addr := cfg.AppHost + ":" + cfg.AppPort
	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("polyglot-go-api listening on http://%s (env=%s)", addr, cfg.AppEnv)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
	log.Println("polyglot-go-api shutdown complete")
}
