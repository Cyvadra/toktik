package polygon

import (
	"fmt"
	"strings"
	"time"

	runtimeconfig "github.com/Cyvadra/toktik/internal/config"
)

const (
	defaultBaseURL          = "https://api.massive.com"
	defaultFlatFilesBaseURL = "https://files.massive.com/flatfiles"
	defaultTimeout          = 60 * time.Second
)

type Config struct {
	APIKey             string
	BaseURL            string
	FlatFilesBaseURL   string
	FlatFilesTool      string
	FlatFilesCacheDir  string
	FlatFilesAccessKey string
	FlatFilesSecretKey string
	Timeout            time.Duration
	Trace              bool
	Pagination         bool
}

func LoadConfigFromEnv() (Config, error) {
	runtimeCfg, err := runtimeconfig.LoadRuntime()
	if err != nil {
		return Config{}, err
	}
	return LoadConfigFromRuntime(runtimeCfg)
}

func LoadConfigFromRuntime(runtimeCfg runtimeconfig.Runtime) (Config, error) {
	apiKey, err := runtimeSecret(runtimeCfg, "polygon.api_key", runtimeCfg.Polygon.APIKey)
	if err != nil {
		return Config{}, err
	}
	flatFilesAccessKey, err := runtimeSecret(runtimeCfg, "polygon.flat_files_access_key", runtimeCfg.Polygon.FlatFilesAccessKey)
	if err != nil {
		return Config{}, err
	}
	flatFilesSecretKey, err := runtimeSecret(runtimeCfg, "polygon.flat_files_secret_key", runtimeCfg.Polygon.FlatFilesSecretKey)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		APIKey:             apiKey,
		BaseURL:            strings.TrimSpace(runtimeCfg.Polygon.BaseURL),
		FlatFilesBaseURL:   strings.TrimSpace(runtimeCfg.Polygon.FlatFilesBaseURL),
		FlatFilesTool:      strings.TrimSpace(runtimeCfg.Polygon.FlatFilesTool),
		FlatFilesCacheDir:  strings.TrimSpace(runtimeCfg.Polygon.FlatFilesCacheDir),
		FlatFilesAccessKey: flatFilesAccessKey,
		FlatFilesSecretKey: flatFilesSecretKey,
		Timeout:            time.Duration(runtimeCfg.Polygon.TimeoutSeconds) * time.Second,
		Trace:              runtimeCfg.Polygon.Trace,
		Pagination:         runtimeCfg.Polygon.Pagination,
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func runtimeSecret(runtimeCfg runtimeconfig.Runtime, field, fallback string) (string, error) {
	if value := strings.TrimSpace(fallback); value != "" {
		return value, nil
	}
	if runtimeCfg.Secrets == nil {
		return "", nil
	}
	value, err := runtimeCfg.Secrets.Open(field)
	if err != nil {
		return "", fmt.Errorf("load %s from runtime secrets: %w", field, err)
	}
	return strings.TrimSpace(value), nil
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.APIKey) == "" {
		return fmt.Errorf("polygon.api_key is required")
	}
	return nil
}

func (c Config) normalizedBaseURL() string {
	if strings.TrimSpace(c.BaseURL) == "" {
		return defaultBaseURL
	}
	return strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
}

func (c Config) normalizedFlatFilesBaseURL() string {
	if strings.TrimSpace(c.FlatFilesBaseURL) == "" {
		return defaultFlatFilesBaseURL
	}
	return strings.TrimRight(strings.TrimSpace(c.FlatFilesBaseURL), "/")
}

func (c Config) normalizedFlatFilesTool() string {
	tool := strings.TrimSpace(strings.ToLower(c.FlatFilesTool))
	if tool == "" {
		return "mc"
	}
	return tool
}

func (c Config) normalizedFlatFilesCacheDir() string {
	return strings.TrimSpace(c.FlatFilesCacheDir)
}

func (c Config) validateFlatFilesConfig() error {
	if strings.TrimSpace(c.FlatFilesCacheDir) == "" {
		return fmt.Errorf("polygon.flat_files_cache_dir is required")
	}
	if strings.TrimSpace(c.FlatFilesAccessKey) == "" {
		return fmt.Errorf("polygon.flat_files_access_key is required")
	}
	if strings.TrimSpace(c.FlatFilesSecretKey) == "" {
		return fmt.Errorf("polygon.flat_files_secret_key is required")
	}
	switch c.normalizedFlatFilesTool() {
	case "mc", "rclone":
		return nil
	default:
		return fmt.Errorf("unsupported polygon flatfile download tool %q; supported tools: mc, rclone", c.FlatFilesTool)
	}
}

func (c Config) normalizedTimeout() time.Duration {
	if c.Timeout <= 0 {
		return defaultTimeout
	}
	return c.Timeout
}
