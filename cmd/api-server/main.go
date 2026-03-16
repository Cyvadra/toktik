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
	"github.com/Cyvadra/toktik/internal/cryptooptions"
	"github.com/Cyvadra/toktik/internal/service"
)

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	dsn := flag.String("clickhouse-dsn", envOrDefault("CLICKHOUSE_DSN", "clickhouse://default:@localhost:9000/default"), "ClickHouse DSN")
	addr := flag.String("addr", envOrDefault("LISTEN_ADDR", ":8080"), "Listen address")
	schemaFile := flag.String("schema", "", "Path to DDL SQL file (auto-detected if empty)")
	flag.Parse()

	if *schemaFile == "" {
		candidates := []string{
			"schema/clickhouse/crypto_options.sql",
			"../schema/clickhouse/crypto_options.sql",
			"../../schema/clickhouse/crypto_options.sql",
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				*schemaFile = c
				break
			}
		}
		if *schemaFile == "" {
			log.Fatalf("cannot find schema SQL file; specify --schema path")
		}
	}

	ctx := context.Background()

	conn, err := cryptooptions.ConnectClickHouse(ctx, *dsn)
	if err != nil {
		log.Fatalf("connect to ClickHouse: %v", err)
	}
	log.Printf("Connected to ClickHouse")

	if err := cryptooptions.InitSchema(ctx, conn, *schemaFile); err != nil {
		log.Fatalf("init schema: %v", err)
	}
	if err := cryptooptions.InitKlineSchema(ctx, conn); err != nil {
		log.Fatalf("init kline schema: %v", err)
	}
	log.Printf("Schema initialized")

	svc := service.NewCryptoOptionsService(conn)
	router := api.NewRouter(svc)

	srv := &http.Server{
		Addr:    *addr,
		Handler: router,
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
