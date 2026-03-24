package main

import (
	"context"
	"flag"
	"log"
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

	ddlFile, err := appCli.ResolveSchemaFile(*schemaFile, appCli.CryptoOptionsSchemaFile)
	if err != nil {
		log.Fatalf("%v", err)
	}

	ctx := context.Background()

	conn, err := appCli.ConnectClickHouse(ctx, *dsn, &appCli.SchemaInit{
		DDLFile:   ddlFile,
		Kline:     true,
		SpotKline: true,
	})
	if err != nil {
		log.Fatalf("%v", err)
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
		log.Printf("Starting API server on %s", *addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("server forced to shutdown: %v", err)
	}
	log.Println("Server exited")
}
