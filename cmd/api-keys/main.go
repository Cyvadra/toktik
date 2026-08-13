package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/internal/apikeyauth"
	"github.com/Cyvadra/toktik/internal/apikeyrepo"
	appCli "github.com/Cyvadra/toktik/internal/cli"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func main() {
	appCli.SetupLogger(false, slog.LevelInfo)
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2:]); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `Usage: api-keys <command> [flags]

Commands:
  create   Create a new API key and print the plaintext token once
  list     List API keys without plaintext tokens
  set-rate-limit  Set an API key's per-key rate limit
  disable  Disable an API key by id
  rotate   Rotate an API key by id and print the new plaintext token once

`)
}

func run(command string, args []string) error {
	switch command {
	case "create":
		return createCommand(args)
	case "list":
		return listCommand(args)
	case "set-rate-limit":
		return setRateLimitCommand(args)
	case "disable":
		return disableCommand(args)
	case "rotate":
		return rotateCommand(args)
	default:
		usage()
		return fmt.Errorf("unknown command %q", command)
	}
}

func openRepo(ctx context.Context) (*apikeyrepo.Repo, func() error, error) {
	cfg := appCli.MustLoadRuntime()
	dsn, err := cfg.MySQLDSN()
	if err != nil {
		return nil, nil, fmt.Errorf("build mysql dsn: %w", err)
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, nil, fmt.Errorf("connect mysql: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, nil, fmt.Errorf("mysql sql db: %w", err)
	}
	repo := apikeyrepo.New(db)
	if err := repo.AutoMigrate(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, nil, fmt.Errorf("migrate API key tables: %w", err)
	}
	return repo, sqlDB.Close, nil
}

func createCommand(args []string) error {
	fs := flag.NewFlagSet("create", flag.ContinueOnError)
	name := fs.String("name", "", "Human-readable API key name")
	ownerType := fs.String("owner-type", "", "Owner type, such as user, business, or service")
	ownerID := fs.String("owner-id", "", "Owner identifier")
	userType := fs.String("user-type", "", "Future user type metadata")
	authLevel := fs.String("auth-level", "", "Future auth level metadata")
	rateLimitRPS := fs.Float64("rate-limit-rps", 0, "Per-key rate limit RPS; <=0 means global default")
	expiresAtRaw := fs.String("expires-at", "", "Expiration time as RFC3339 or YYYY-MM-DD; empty means never")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*name) == "" {
		return fmt.Errorf("--name is required")
	}
	expiresAt, err := parseOptionalTime(*expiresAtRaw)
	if err != nil {
		return err
	}
	token, prefix, digest, err := apikeyauth.GenerateToken()
	if err != nil {
		return fmt.Errorf("generate API key: %w", err)
	}
	var limit *float64
	if *rateLimitRPS > 0 {
		value := *rateLimitRPS
		limit = &value
	}
	ctx := context.Background()
	repo, closeDB, err := openRepo(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = closeDB() }()
	record := &apikeyrepo.APIKey{
		Name:         strings.TrimSpace(*name),
		KeyDigest:    digest,
		KeyPrefix:    prefix,
		OwnerType:    strings.TrimSpace(*ownerType),
		OwnerID:      strings.TrimSpace(*ownerID),
		UserType:     strings.TrimSpace(*userType),
		AuthLevel:    strings.TrimSpace(*authLevel),
		RateLimitRPS: limit,
		ExpiresAt:    expiresAt,
		Active:       true,
	}
	if err := repo.Create(ctx, record); err != nil {
		return fmt.Errorf("create API key: %w", err)
	}
	fmt.Printf("id=%d\n", record.ID)
	fmt.Printf("prefix=%s\n", record.KeyPrefix)
	fmt.Printf("api_key=%s\n", token)
	return nil
}

func listCommand(args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	activeOnly := fs.Bool("active-only", false, "Only list active keys")
	ownerType := fs.String("owner-type", "", "Filter by owner type")
	ownerID := fs.String("owner-id", "", "Filter by owner id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx := context.Background()
	repo, closeDB, err := openRepo(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = closeDB() }()
	keys, err := repo.List(ctx, apikeyrepo.ListFilter{ActiveOnly: *activeOnly, OwnerType: strings.TrimSpace(*ownerType), OwnerID: strings.TrimSpace(*ownerID)})
	if err != nil {
		return fmt.Errorf("list API keys: %w", err)
	}
	fmt.Println("id\tactive\tprefix\tname\towner\tuser_type\tauth_level\trate_limit_rps\texpires_at\tlast_used_at")
	for _, key := range keys {
		fmt.Printf("%d\t%t\t%s\t%s\t%s/%s\t%s\t%s\t%s\t%s\t%s\n",
			key.ID,
			key.Active,
			key.KeyPrefix,
			key.Name,
			key.OwnerType,
			key.OwnerID,
			key.UserType,
			key.AuthLevel,
			formatOptionalFloat(key.RateLimitRPS),
			formatOptionalTime(key.ExpiresAt),
			formatOptionalTime(key.LastUsedAt),
		)
	}
	return nil
}

func setRateLimitCommand(args []string) error {
	fs := flag.NewFlagSet("set-rate-limit", flag.ContinueOnError)
	id := fs.Uint64("id", 0, "API key id to update")
	rateLimitRPS := fs.Float64("rate-limit-rps", 0, "Per-key rate limit RPS; must be greater than zero")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *id == 0 {
		return fmt.Errorf("--id is required")
	}
	if *rateLimitRPS <= 0 {
		return fmt.Errorf("--rate-limit-rps must be greater than zero")
	}
	ctx := context.Background()
	repo, closeDB, err := openRepo(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = closeDB() }()
	ok, err := repo.SetRateLimit(ctx, *id, *rateLimitRPS)
	if err != nil {
		return fmt.Errorf("set API key rate limit: %w", err)
	}
	if !ok {
		return fmt.Errorf("API key id %d not found", *id)
	}
	fmt.Printf("id=%d\nrate_limit_rps=%g\n", *id, *rateLimitRPS)
	return nil
}

func disableCommand(args []string) error {
	fs := flag.NewFlagSet("disable", flag.ContinueOnError)
	id := fs.Uint64("id", 0, "API key id to disable")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *id == 0 {
		return fmt.Errorf("--id is required")
	}
	ctx := context.Background()
	repo, closeDB, err := openRepo(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = closeDB() }()
	ok, err := repo.Disable(ctx, *id)
	if err != nil {
		return fmt.Errorf("disable API key: %w", err)
	}
	if !ok {
		return fmt.Errorf("API key id %d not found", *id)
	}
	fmt.Printf("disabled id=%d\n", *id)
	return nil
}

func rotateCommand(args []string) error {
	fs := flag.NewFlagSet("rotate", flag.ContinueOnError)
	id := fs.Uint64("id", 0, "API key id to rotate")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *id == 0 {
		return fmt.Errorf("--id is required")
	}
	token, prefix, digest, err := apikeyauth.GenerateToken()
	if err != nil {
		return fmt.Errorf("generate API key: %w", err)
	}
	ctx := context.Background()
	repo, closeDB, err := openRepo(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = closeDB() }()
	ok, err := repo.Rotate(ctx, *id, digest, prefix)
	if err != nil {
		return fmt.Errorf("rotate API key: %w", err)
	}
	if !ok {
		return fmt.Errorf("API key id %d not found", *id)
	}
	fmt.Printf("id=%d\n", *id)
	fmt.Printf("prefix=%s\n", prefix)
	fmt.Printf("api_key=%s\n", token)
	return nil
}

func parseOptionalTime(value string) (*time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || strings.EqualFold(trimmed, "null") {
		return nil, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		parsed, err := time.Parse(layout, trimmed)
		if err == nil {
			return &parsed, nil
		}
	}
	return nil, fmt.Errorf("invalid --expires-at %q: use RFC3339 or YYYY-MM-DD", value)
}

func formatOptionalTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.Format(time.RFC3339)
}

func formatOptionalFloat(value *float64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatFloat(*value, 'f', -1, 64)
}
