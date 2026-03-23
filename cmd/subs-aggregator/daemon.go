package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"subs-aggregator/internal/config"
	"subs-aggregator/internal/handler"
	"subs-aggregator/internal/service"
	"subs-aggregator/internal/xui"
)

func defaultConfigPath() string {
	etcPath := "/etc/" + appName + "/config.yaml"
	if _, err := os.Stat(etcPath); err == nil {
		return etcPath
	}
	return "configs/config.yaml"
}

func pidFilePath() string {
	return "/tmp/" + appName + ".pid"
}

func logFilePath() string {
	return "/var/log/" + appName + ".log"
}

func checkRunning(pidFile string) (int, bool) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return pid, false
	}
	err = process.Signal(syscall.Signal(0))
	return pid, err == nil
}

func cmdRun(cfgPath string) {
	cfg, err := config.Load(cfgPath)
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
	adminHandler := handler.NewAdminHandler(clientService, logger, cfg.Server.AdminSecret)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /sub/{subId}", subHandler.HandleSubscription)
	mux.HandleFunc("GET /admin/inbounds", adminHandler.HandleListInbounds)
	mux.HandleFunc("GET /admin/sub-url/{name}", adminHandler.HandleGetSubURL)
	mux.HandleFunc("POST /admin/clients", adminHandler.HandleCreateClient)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
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
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// Write PID file for process management
	os.WriteFile(pidFilePath(), []byte(strconv.Itoa(os.Getpid())), 0644)

	sig := <-sigCh
	logger.Info("received signal, shutting down", "signal", sig)

	os.Remove(pidFilePath())

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", "error", err)
		os.Exit(1)
	}

	logger.Info("server stopped")
}

func cmdStart(cfgPath string) {
	pidFile := pidFilePath()
	if pid, running := checkRunning(pidFile); running {
		fmt.Printf("%s is already running (PID %d)\n", appName, pid)
		os.Exit(1)
	}

	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to find executable path: %v\n", err)
		os.Exit(1)
	}

	logFile, err := os.OpenFile(logFilePath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open log file %s: %v\n", logFilePath(), err)
		os.Exit(1)
	}
	defer logFile.Close()

	cmd := exec.Command(exe, "run", "--config", cfgPath)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to start: %v\n", err)
		os.Exit(1)
	}

	// Wait briefly for the child to write PID file
	time.Sleep(500 * time.Millisecond)

	if pid, running := checkRunning(pidFile); running {
		fmt.Printf("%s started (PID %d)\n", appName, pid)
	} else {
		fmt.Fprintf(os.Stderr, "%s may have failed to start, check logs: %s\n", appName, logFilePath())
		os.Exit(1)
	}
}

func cmdStop() {
	pidFile := pidFilePath()
	pid, running := checkRunning(pidFile)
	if !running {
		fmt.Printf("%s is not running\n", appName)
		os.Exit(1)
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to find process %d: %v\n", pid, err)
		os.Exit(1)
	}

	fmt.Printf("stopping %s (PID %d)...\n", appName, pid)

	if err := process.Signal(syscall.SIGTERM); err != nil {
		fmt.Fprintf(os.Stderr, "failed to send SIGTERM: %v\n", err)
		os.Exit(1)
	}

	for i := 0; i < 10; i++ {
		time.Sleep(1 * time.Second)
		if err := process.Signal(syscall.Signal(0)); err != nil {
			fmt.Printf("%s stopped\n", appName)
			os.Remove(pidFile)
			return
		}
	}

	fmt.Printf("%s did not stop gracefully, forcing...\n", appName)
	process.Signal(syscall.SIGKILL)
	os.Remove(pidFile)
	fmt.Printf("%s killed\n", appName)
}

func cmdStatus() {
	pid, running := checkRunning(pidFilePath())
	if running {
		fmt.Printf("%s is running (PID %d)\n", appName, pid)
	} else {
		fmt.Printf("%s is not running\n", appName)
	}
}

func cmdRestart(cfgPath string) {
	pidFile := pidFilePath()
	if pid, running := checkRunning(pidFile); running {
		process, err := os.FindProcess(pid)
		if err == nil {
			fmt.Printf("stopping %s (PID %d)...\n", appName, pid)
			process.Signal(syscall.SIGTERM)
			for i := 0; i < 10; i++ {
				time.Sleep(1 * time.Second)
				if err := process.Signal(syscall.Signal(0)); err != nil {
					break
				}
			}
			if err := process.Signal(syscall.Signal(0)); err == nil {
				process.Signal(syscall.SIGKILL)
			}
			os.Remove(pidFile)
			fmt.Printf("%s stopped\n", appName)
		}
	}

	cmdStart(cfgPath)
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
	var level slog.Level
	if err := level.UnmarshalText([]byte(s)); err != nil {
		return slog.LevelInfo
	}
	return level
}
