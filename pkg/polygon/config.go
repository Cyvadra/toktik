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
	defaultRESTQPS          = 4.0
	defaultRESTBurst        = 1
	defaultRetryAttempts    = 4
	defaultRetryBaseDelay   = 500 * time.Millisecond
	defaultRetryMaxDelay    = 8 * time.Second
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
	RESTQPS            float64
	RESTBurst          int
	RetryAttempts      int
	RetryBaseDelay     time.Duration
	RetryMaxDelay      time.Duration
}

func LoadConfigFromEnv() (Config, error) {
	runtimeCfg, err := runtimeconfig.LoadRuntime()
	if err != nil {
		return Config{}, err
	}
	return LoadConfigFromRuntime(runtimeCfg)
}

func LoadConfigFromRuntime(runtimeCfg runtimeconfig.Runtime) (Config, error) {
	apiKey, err := runtimeCfg.PolygonAPIKey()
	if err != nil {
		return Config{}, err
	}
	flatFilesAccessKey, err := runtimeCfg.PolygonFlatFilesAccessKey()
	if err != nil {
		return Config{}, err
	}
	flatFilesSecretKey, err := runtimeCfg.PolygonFlatFilesSecretKey()
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
		RESTQPS:            runtimeCfg.Polygon.RESTQPS,
		RESTBurst:          runtimeCfg.Polygon.RESTBurst,
		RetryAttempts:      runtimeCfg.Polygon.RetryAttempts,
		RetryBaseDelay:     time.Duration(runtimeCfg.Polygon.RetryBaseDelayMS) * time.Millisecond,
		RetryMaxDelay:      time.Duration(runtimeCfg.Polygon.RetryMaxDelayMS) * time.Millisecond,
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
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

func (c Config) normalizedRESTQPS() float64 {
	if c.RESTQPS <= 0 {
		return defaultRESTQPS
	}
	return c.RESTQPS
}

func (c Config) normalizedRESTBurst() int {
	if c.RESTBurst <= 0 {
		return defaultRESTBurst
	}
	return c.RESTBurst
}

func (c Config) normalizedRetryAttempts() int {
	if c.RetryAttempts <= 0 {
		return defaultRetryAttempts
	}
	return c.RetryAttempts
}

func (c Config) normalizedRetryBaseDelay() time.Duration {
	if c.RetryBaseDelay <= 0 {
		return defaultRetryBaseDelay
	}
	return c.RetryBaseDelay
}

func (c Config) normalizedRetryMaxDelay() time.Duration {
	if c.RetryMaxDelay <= 0 {
		return defaultRetryMaxDelay
	}
	return c.RetryMaxDelay
}
