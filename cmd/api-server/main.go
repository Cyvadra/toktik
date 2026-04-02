package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Cyvadra/toktik/internal/api"
	appCli "github.com/Cyvadra/toktik/internal/cli"
	"github.com/Cyvadra/toktik/internal/service"
)

func main() {
	dsn := flag.String("clickhouse-dsn", appCli.EnvOrDefault("CLICKHOUSE_DSN", appCli.DefaultDSN), "ClickHouse DSN")
	addr := flag.String("addr", appCli.EnvOrDefault("LISTEN_ADDR", ":8080"), "Listen address")
	schemaFile := flag.String("schema", "", "Path to DDL SQL file (auto-detected if empty)")
	flag.Parse()

	appCli.SetupLogger(true, slog.LevelInfo)

	ddlFile, err := appCli.ResolveSchemaFile(*schemaFile, appCli.CryptoOptionsSchemaFile)
	if err != nil {
		slog.Error("resolve schema", "error", err)
		os.Exit(1)
	}

	ctx := context.Background()

	conn, err := appCli.ConnectClickHouse(ctx, *dsn, &appCli.SchemaInit{
		DDLFile:    ddlFile,
		Kline:      true,
		SpotKline:  true,
		ChainCache: true,
	})
	if err != nil {
		slog.Error("connect clickhouse", "error", err)
		os.Exit(1)
	}

	svc := service.NewCryptoOptionsService(conn)
	router := api.NewRouter(svc)

	srv := &http.Server{
		Addr:              *addr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Start server in background
	go func() {
		slog.Info("starting API server", "addr", *addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("shutting down server")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server forced to shutdown", "error", err)
		os.Exit(1)
	}
	slog.Info("server exited")
}
