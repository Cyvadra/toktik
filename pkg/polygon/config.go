package polygon

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	EnvMassiveAPIKey            = "MASSIVE_API_KEY"
	EnvPolygonAPIKey            = "POLYGON_API_KEY"
	EnvMassiveBaseURL           = "MASSIVE_BASE_URL"
	EnvPolygonBaseURL           = "POLYGON_BASE_URL"
	EnvMassiveFlatFilesBaseURL  = "MASSIVE_FLATFILES_BASE_URL"
	EnvPolygonFlatFilesBaseURL  = "POLYGON_FLATFILES_BASE_URL"
	EnvMassiveFlatFilesCacheDir = "MASSIVE_FLATFILES_CACHE_DIR"
	EnvPolygonFlatFilesCacheDir = "POLYGON_FLATFILES_CACHE_DIR"
	EnvMassiveTimeoutSeconds    = "MASSIVE_TIMEOUT_SECONDS"
	EnvPolygonTimeoutSeconds    = "POLYGON_TIMEOUT_SECONDS"
	EnvMassiveTrace             = "MASSIVE_TRACE"
	EnvPolygonTrace             = "POLYGON_TRACE"
	EnvMassivePagination        = "MASSIVE_PAGINATION"
	EnvPolygonPagination        = "POLYGON_PAGINATION"
	defaultBaseURL              = "https://api.massive.com"
	defaultFlatFilesBaseURL     = "https://files.massive.com/flatfiles"
	defaultTimeout              = 60 * time.Second
)

type Config struct {
	APIKey            string
	BaseURL           string
	FlatFilesBaseURL  string
	FlatFilesCacheDir string
	Timeout           time.Duration
	Trace             bool
	Pagination        bool
}

func LoadConfigFromEnv() (Config, error) {
	cfg := Config{
		APIKey:            firstEnv(EnvMassiveAPIKey, EnvPolygonAPIKey),
		BaseURL:           firstEnv(EnvMassiveBaseURL, EnvPolygonBaseURL),
		FlatFilesBaseURL:  firstEnv(EnvMassiveFlatFilesBaseURL, EnvPolygonFlatFilesBaseURL),
		FlatFilesCacheDir: firstEnv(EnvMassiveFlatFilesCacheDir, EnvPolygonFlatFilesCacheDir),
		Pagination:        true,
	}

	if timeoutValue := firstEnv(EnvMassiveTimeoutSeconds, EnvPolygonTimeoutSeconds); timeoutValue != "" {
		seconds, err := strconv.Atoi(timeoutValue)
		if err != nil || seconds <= 0 {
			return Config{}, fmt.Errorf("invalid timeout value %q", timeoutValue)
		}
		cfg.Timeout = time.Duration(seconds) * time.Second
	}

	if traceValue := firstEnv(EnvMassiveTrace, EnvPolygonTrace); traceValue != "" {
		trace, err := strconv.ParseBool(traceValue)
		if err != nil {
			return Config{}, fmt.Errorf("invalid trace value %q", traceValue)
		}
		cfg.Trace = trace
	}

	if paginationValue := firstEnv(EnvMassivePagination, EnvPolygonPagination); paginationValue != "" {
		pagination, err := strconv.ParseBool(paginationValue)
		if err != nil {
			return Config{}, fmt.Errorf("invalid pagination value %q", paginationValue)
		}
		cfg.Pagination = pagination
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.APIKey) == "" {
		return fmt.Errorf("missing required polygon environment variable: %s or %s", EnvMassiveAPIKey, EnvPolygonAPIKey)
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

func (c Config) normalizedFlatFilesCacheDir() string {
	return strings.TrimSpace(c.FlatFilesCacheDir)
}

func (c Config) validateFlatFilesConfig() error {
	if strings.TrimSpace(c.FlatFilesCacheDir) == "" {
		return fmt.Errorf("missing required polygon environment variable: %s or %s", EnvMassiveFlatFilesCacheDir, EnvPolygonFlatFilesCacheDir)
	}
	return nil
}

func (c Config) normalizedTimeout() time.Duration {
	if c.Timeout <= 0 {
		return defaultTimeout
	}
	return c.Timeout
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}
