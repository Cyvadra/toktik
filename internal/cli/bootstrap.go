package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/cryptooptions"
)

// SetupLogger configures the default slog logger.
// If jsonOutput is true a JSON handler is used (suitable for servers);
// otherwise a text handler is used (suitable for CLI tools).
func SetupLogger(jsonOutput bool, level slog.Level) {
	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if jsonOutput {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		handler = slog.NewTextHandler(os.Stderr, opts)
	}
	slog.SetDefault(slog.New(handler))
}

// SchemaInit controls which schema initialization steps to run.
type SchemaInit struct {
	// DDLFile is the path to the base DDL SQL file. If empty no base schema is applied.
	DDLFile string
	// Kline enables crypto_options kline materialized views.
	Kline bool
	// SpotKline enables crypto_spot kline materialized views.
	SpotKline bool
}

// ConnectClickHouse establishes a ClickHouse connection, initialises the
// requested schemas, and returns the connection. It is the single entry-point
// for all CLI tools that talk to ClickHouse.
func ConnectClickHouse(ctx context.Context, dsn string, schema *SchemaInit) (driver.Conn, error) {
	conn, err := cryptooptions.ConnectClickHouse(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect to ClickHouse: %w", err)
	}
	slog.Info("Connected to ClickHouse")

	if schema == nil {
		return conn, nil
	}

	if schema.DDLFile != "" {
		if err := cryptooptions.InitSchema(ctx, conn, schema.DDLFile); err != nil {
			return nil, fmt.Errorf("init schema: %w", err)
		}
	}
	if schema.Kline {
		if err := cryptooptions.InitKlineSchema(ctx, conn); err != nil {
			return nil, fmt.Errorf("init kline schema: %w", err)
		}
	}
	if schema.SpotKline {
		if err := cryptooptions.InitSpotKlineSchema(ctx, conn); err != nil {
			return nil, fmt.Errorf("init spot kline schema: %w", err)
		}
	}
	slog.Info("Schema initialized")
	return conn, nil
}

// FindSchemaFile probes standard relative paths for a DDL SQL file and returns
// the first match. The candidates list defines which paths to try. If none are
// found an error is returned.
func FindSchemaFile(candidates []string) (string, error) {
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", fmt.Errorf("cannot find schema SQL file; specify --schema path")
}

// CryptoOptionsSchemaFile returns the path to the crypto_options DDL file by
// probing the standard locations. It wraps FindSchemaFile with the default
// candidate list used across crypto-options CLI tools.
func CryptoOptionsSchemaFile() (string, error) {
	return FindSchemaFile([]string{
		"schema/clickhouse/crypto_options.sql",
		"../schema/clickhouse/crypto_options.sql",
		"../../schema/clickhouse/crypto_options.sql",
	})
}

// EquityOptionsSchemaFile returns the path to the equity_options DDL file.
func EquityOptionsSchemaFile() (string, error) {
	return FindSchemaFile([]string{
		"schema/clickhouse/equity_options.sql",
		"../schema/clickhouse/equity_options.sql",
		"../../schema/clickhouse/equity_options.sql",
	})
}

// EnvOrDefault returns the value of an environment variable, or the fallback
// if the variable is empty or unset.
func EnvOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ParseDate parses a "YYYY-MM-DD" string or fatally exits with a descriptive
// message including the flag name.
func ParseDate(value, flagName string) time.Time {
	t, err := time.Parse("2006-01-02", value)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid %s %q: %v\n", flagName, value, err)
		os.Exit(1)
	}
	return t
}

// MustParseDate is like ParseDate but returns an error instead of exiting.
func MustParseDate(value string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date %q: %w", value, err)
	}
	return t, nil
}

// DefaultDSN is the standard ClickHouse DSN default used across CLI tools.
const DefaultDSN = "clickhouse://default:@localhost:9000/default"

// ResolveSchemaFile returns the explicit value if non-empty, otherwise
// auto-detects via the given finder function.
func ResolveSchemaFile(explicit string, finder func() (string, error)) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	return finder()
}
