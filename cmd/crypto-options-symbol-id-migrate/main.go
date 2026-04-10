package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	appCli "github.com/Cyvadra/toktik/internal/cli"
	"github.com/Cyvadra/toktik/internal/cryptooptions"
)

func main() {
	runtimeCfg := appCli.MustLoadRuntime()
	dsn := flag.String("clickhouse-dsn", runtimeCfg.ClickHouse.DSN, "ClickHouse DSN")
	schemaFile := flag.String("schema", "", "Path to DDL SQL file (auto-detected if empty)")
	backupSuffix := flag.String("backup-suffix", "", "Suffix for retained CRC32 backup tables; defaults to current UTC timestamp")
	dryRun := flag.Bool("dry-run", false, "Print the intended migration plan without executing DDL or data backfill")
	flag.Parse()

	ddlFile, err := appCli.ResolveSchemaFile(*schemaFile, appCli.CryptoOptionsSchemaFile)
	if err != nil {
		log.Fatalf("%v", err)
	}

	ctx := context.Background()
	conn, err := appCli.ConnectClickHouse(ctx, *dsn, nil)
	if err != nil {
		log.Fatalf("%v", err)
	}

	opts := cryptooptions.SymbolIDMigrationOptions{
		DDLPath:      ddlFile,
		BackupSuffix: *backupSuffix,
		DryRun:       *dryRun,
	}
	if err := cryptooptions.MigrateCryptoOptionSymbolIDs(ctx, conn, opts); err != nil {
		log.Fatalf("migrate crypto option symbol IDs: %v", err)
	}

	if *dryRun {
		fmt.Fprintln(os.Stdout, "Crypto option symbol ID migration dry-run completed")
		return
	}
	fmt.Fprintln(os.Stdout, "Crypto option symbol ID migration completed")
}
