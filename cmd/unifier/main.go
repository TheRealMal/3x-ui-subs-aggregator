package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"3x-ui-sub-unifier/internal/config"
	"3x-ui-sub-unifier/internal/handler"
	"3x-ui-sub-unifier/internal/service"
	"3x-ui-sub-unifier/internal/xui"
)

func main() {
	configPath := flag.String("config", "configs/config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	level := parseLogLevel(cfg.Log.Level)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	xuiClients := make([]*xui.APIClient, len(cfg.Panels))
	for i, panel := range cfg.Panels {
		xuiClients[i] = xui.NewAPIClient(panel, logger)
	}

	ctx := context.Background()
	for _, client := range xuiClients {
		if err := client.Login(ctx); err != nil {
			logger.Error("failed to login to panel", "panel", client.PanelName(), "error", err)
			os.Exit(1)
		}
	}

	subService := service.NewSubscriptionService(xuiClients, logger)
	clientService := service.NewClientService(xuiClients, logger)

	subHandler := handler.NewSubscriptionHandler(subService, logger)
	adminHandler := handler.NewAdminHandler(subService, clientService, logger)

	mux := http.NewServeMux()
	mux.HandleFunc("/sub/", subHandler.HandleSubscription)
	mux.HandleFunc("/admin/sub-url/", adminHandler.HandleGetSubURL)
	mux.HandleFunc("/admin/clients", adminHandler.HandleCreateClient)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:      withLogging(logger, mux),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		logger.Info("starting server", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	sig := <-sigCh
	logger.Info("received signal, shutting down", "signal", sig)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", "error", err)
		os.Exit(1)
	}

	logger.Info("server stopped")
}

func withLogging(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		logger.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"remote", r.RemoteAddr,
			"duration", time.Since(start),
		)
	})
}

func parseLogLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
