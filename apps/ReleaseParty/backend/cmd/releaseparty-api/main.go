package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"releaseparty/internal/api"
	"releaseparty/internal/config"
	"releaseparty/internal/githubapp"
	"releaseparty/internal/store"
)

func main() {
	logger := log.New(os.Stdout, "releaseparty-api ", log.LstdFlags|log.LUTC)

	cfg, err := config.Load()
	if err != nil {
		logger.Fatalf("config: %v", err)
	}
	app, err := githubapp.New(cfg.GitHubAppID, cfg.GitHubAppSlug, cfg.GitHubWebhookSecret, cfg.GitHubPrivateKeyPEM, cfg.BaseURL)
	if err != nil {
		logger.Fatalf("github app: %v", err)
	}
	st, err := store.Open(cfg.DatabasePath)
	if err != nil {
		logger.Fatalf("db: %v", err)
	}
	defer st.Close()

	srv := api.New(cfg, app, st, logger)

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Router(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Printf("listening on %s", cfg.Addr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("server: %v", err)
		}
	}()

	stop := make(chan os.Signal, 2)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	<-stop
	logger.Printf("shutting down...")
	_ = httpSrv.Close()
}

