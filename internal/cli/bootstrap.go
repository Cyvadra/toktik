package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/config"
	"github.com/Cyvadra/toktik/internal/cryptooptions"
	"github.com/Cyvadra/toktik/internal/usmarket"
	"github.com/Cyvadra/toktik/pkg/fmp"
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
	// ChainCache enables crypto option-chain cache materialized views.
	ChainCache bool
	// OptionWall enables the US options option-wall storage table.
	OptionWall bool
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
	if schema.ChainCache {
		if err := cryptooptions.InitChainCacheSchema(ctx, conn); err != nil {
			return nil, fmt.Errorf("init chain cache schema: %w", err)
		}
	}
	if schema.OptionWall {
		if err := usmarket.InitOptionWallSchema(ctx, conn); err != nil {
			return nil, fmt.Errorf("init option wall schema: %w", err)
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

func MustLoadRuntime() config.Runtime {
	cfg, err := config.LoadRuntime()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load runtime config: %v\n", err)
		os.Exit(1)
	}
	fmp.SetDefaultCacheDir(cfg.FMP.CacheDir)
	return cfg
}

// CryptoOptionsSchemaFile returns the path to the crypto_options DDL file by
// probing the standard locations. It wraps FindSchemaFile with the default
// candidate list used across crypto-options CLI tools.
func CryptoOptionsSchemaFile() (string, error) {
	return FindSchemaFile(schemaCandidates("crypto_options.sql"))
}

// UsMarketSchemaFile returns the path to the us_market DDL file.
func UsMarketSchemaFile() (string, error) {
	return FindSchemaFile(schemaCandidates("us_market.sql"))
}

// ForexMarketSchemaFile returns the path to the forex_market DDL file.
func ForexMarketSchemaFile() (string, error) {
	return FindSchemaFile(schemaCandidates("forex_market.sql"))
}

// FeatureStoreSchemaFile returns the path to the feature_store DDL file.
func FeatureStoreSchemaFile() (string, error) {
	return FindSchemaFile(schemaCandidates("feature_store.sql"))
}

// FundamentalsSchemaFile returns the path to the fundamentals DDL file.
func FundamentalsSchemaFile() (string, error) {
	return FindSchemaFile(schemaCandidates("fundamentals.sql"))
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

func schemaCandidates(fileName string) []string {
	cfg, err := config.LoadRuntime()
	if err == nil {
		return cfg.SchemaPathCandidates(fileName)
	}
	return []string{
		filepath.Join("schema", "clickhouse", fileName),
		filepath.Join("..", "schema", "clickhouse", fileName),
		filepath.Join("..", "..", "schema", "clickhouse", fileName),
	}
}
