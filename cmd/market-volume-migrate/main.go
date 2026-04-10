package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	appCli "github.com/Cyvadra/toktik/internal/cli"
	"github.com/Cyvadra/toktik/internal/cryptooptions"
	"github.com/Cyvadra/toktik/internal/usmarket"
)

func main() {
	runtimeCfg := appCli.MustLoadRuntime()
	dsn := flag.String("clickhouse-dsn", runtimeCfg.ClickHouse.DSN, "ClickHouse DSN")
	skipCrypto := flag.Bool("skip-crypto", false, "Skip crypto market volume migration")
	skipUS := flag.Bool("skip-us", false, "Skip US market volume migration")
	dryRun := flag.Bool("dry-run", false, "Print the intended migration plan without executing DDL or rebuilds")
	flag.Parse()

	ctx := context.Background()
	cryptoConn, err := appCli.ConnectClickHouse(ctx, *dsn, nil)
	if err != nil {
		log.Fatalf("%v", err)
	}

	if !*skipCrypto {
		if err := cryptooptions.MigrateCryptoVolumeSchema(ctx, cryptoConn, cryptooptions.VolumeSchemaMigrationOptions{DryRun: *dryRun}); err != nil {
			log.Fatalf("migrate crypto volume schema: %v", err)
		}
	}

	if !*skipUS {
		usConn, err := usmarket.ConnectClickHouse(ctx, *dsn)
		if err != nil {
			log.Fatalf("connect US market ClickHouse: %v", err)
		}
		if err := usmarket.MigrateUSVolumeSchema(ctx, usConn, usmarket.VolumeSchemaMigrationOptions{DryRun: *dryRun}); err != nil {
			log.Fatalf("migrate US volume schema: %v", err)
		}
	}

	if *dryRun {
		fmt.Fprintln(os.Stdout, "Market volume migration dry-run completed")
		return
	}
	fmt.Fprintln(os.Stdout, "Market volume migration completed")
}
