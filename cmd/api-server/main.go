package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/Cyvadra/toktik/internal/api"
	"github.com/Cyvadra/toktik/internal/cryptooptions"
	"github.com/Cyvadra/toktik/internal/service"
)

func main() {
	dsn := flag.String("clickhouse-dsn", "clickhouse://default:@localhost:9000/default", "ClickHouse DSN")
	addr := flag.String("addr", ":8080", "Listen address")
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

	log.Printf("Starting API server on %s", *addr)
	if err := router.Run(*addr); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}
