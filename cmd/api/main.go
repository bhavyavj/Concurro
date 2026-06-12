package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bhavyavj/Concurro/internal/api"
	"github.com/bhavyavj/Concurro/internal/config"
	"github.com/bhavyavj/Concurro/internal/processor"
	"github.com/bhavyavj/Concurro/internal/store"
	"github.com/bhavyavj/Concurro/internal/worker"
)

func main() {
	cfg := config.Load()

	// Structured logger
	level := slog.LevelInfo
	if cfg.LogLevel == "debug" {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))

	logger.Info("starting Concurro", "addr", cfg.Addr, "db", cfg.DBPath, "default_workers", cfg.DefaultWorkers)

	// Persistence
	st, err := store.NewSQLiteStore(cfg.DBPath, logger)
	if err != nil {
		logger.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer st.Close()

	// Processors
	procReg := processor.NewRegistry()

	// The star: high-concurrency worker pool
	pool := worker.NewPool(cfg.DefaultWorkers, procReg, st, logger)

	// HTTP server
	router := api.NewRouter(st, pool, logger)

	srv := &http.Server{
		Addr:         cfg.Addr,
		Handler:      router,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}

	// Start the worker pool in the background (inside the same process — great for demos)
	poolCtx, poolCancel := context.WithCancel(context.Background())
	go func() {
		pool.Start(poolCtx)
	}()

	// Start HTTP server
	go func() {
		logger.Info("http server listening", "addr", cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", "error", err)
		}
	}()

	// Graceful shutdown on SIGINT / SIGTERM
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down...")

	// 1. Stop accepting new HTTP requests
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("http shutdown error", "error", err)
	}

	// 2. Signal the worker pool to drain
	poolCancel()
	pool.Shutdown()

	// 3. Close store
	st.Close()

	logger.Info("shutdown complete")
}
