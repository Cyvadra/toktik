package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	EnvConfigPath            = "TOKTIK_CONFIG"
	EnvClickHouseDSN         = "CLICKHOUSE_DSN"
	EnvListenAddr            = "LISTEN_ADDR"
	EnvCORSOrigins           = "CORS_ORIGINS"
	EnvAPIKeys               = "API_KEYS"
	EnvRateLimitRPS          = "RATE_LIMIT_RPS"
	EnvSchemaDir             = "TOKTIK_SCHEMA_DIR"
	EnvDeribitBaseURL        = "DERIBIT_BASE_URL"
	EnvTigerID               = "TIGEROPEN_TIGER_ID"
	EnvTigerPrivateKey       = "TIGEROPEN_PRIVATE_KEY"
	EnvTigerAccount          = "TIGEROPEN_ACCOUNT"
	EnvTigerLicense          = "TIGEROPEN_LICENSE"
	EnvTigerEnvironment      = "TIGEROPEN_ENV"
	EnvTigerLanguage         = "TIGEROPEN_LANGUAGE"
	EnvTigerTimezone         = "TIGEROPEN_TIMEZONE"
	EnvTigerTimeoutSec       = "TIGEROPEN_TIMEOUT_SECONDS"
	EnvTigerDynamicDomain    = "TIGEROPEN_ENABLE_DYNAMIC_DOMAIN"
	EnvTigerToken            = "TIGEROPEN_TOKEN"
	EnvTigerTokenFile        = "TIGEROPEN_TOKEN_FILE"
	EnvTigerServerURL        = "TIGEROPEN_SERVER_URL"
	EnvTigerDeviceID         = "TIGEROPEN_DEVICE_ID"
	EnvRedisEnabled          = "TOKTIK_REDIS_ENABLED"
	EnvRedisAddr             = "TOKTIK_REDIS_ADDR"
	EnvRedisPassword         = "TOKTIK_REDIS_PASSWORD"
	EnvRedisDB               = "TOKTIK_REDIS_DB"
	EnvRedisKeyPrefix        = "TOKTIK_REDIS_KEY_PREFIX"
	EnvRedisDialTimeoutSec   = "TOKTIK_REDIS_DIAL_TIMEOUT_SECONDS"
	EnvRedisReadTimeoutSec   = "TOKTIK_REDIS_READ_TIMEOUT_SECONDS"
	EnvRedisWriteTimeoutSec  = "TOKTIK_REDIS_WRITE_TIMEOUT_SECONDS"
	defaultConfigPath        = "toktik.yaml"
	defaultClickHouseDSN     = "clickhouse://default:@localhost:9000/default"
	defaultListenAddr        = ":9010"
	defaultSchemaDir         = "schema/clickhouse"
	defaultDeribitBaseURL    = "https://www.deribit.com"
	defaultRedisKeyPrefix    = "toktik"
	defaultRedisAddr         = "127.0.0.1:6379"
	defaultPolygonTimeoutSec = 60
)

type Runtime struct {
	ClickHouse ClickHouse `yaml:"clickhouse"`
	APIServer  APIServer  `yaml:"api_server"`
	API        API        `yaml:"api"`
	Paths      Paths      `yaml:"paths"`
	Deribit    Deribit    `yaml:"deribit"`
	Tiger      Tiger      `yaml:"tiger"`
	Polygon    Polygon    `yaml:"polygon"`
	Redis      Redis      `yaml:"redis"`
}

type ClickHouse struct {
	DSN string `yaml:"dsn"`
}

type APIServer struct {
	ListenAddr               string `yaml:"listen_addr"`
	ReadHeaderTimeoutSeconds int    `yaml:"read_header_timeout_seconds"`
	ReadTimeoutSeconds       int    `yaml:"read_timeout_seconds"`
	WriteTimeoutSeconds      int    `yaml:"write_timeout_seconds"`
	IdleTimeoutSeconds       int    `yaml:"idle_timeout_seconds"`
}

type API struct {
	CORSOrigins  []string `yaml:"cors_origins"`
	APIKeys      []string `yaml:"api_keys"`
	RateLimitRPS float64  `yaml:"rate_limit_rps"`
}

type Paths struct {
	SchemaDir string `yaml:"schema_dir"`
}

type Deribit struct {
	BaseURL string `yaml:"base_url"`
}

type Tiger struct {
	TigerID             string `yaml:"tiger_id"`
	PrivateKey          string `yaml:"private_key"`
	Account             string `yaml:"account"`
	License             string `yaml:"license"`
	Environment         string `yaml:"environment"`
	Language            string `yaml:"language"`
	Timezone            string `yaml:"timezone"`
	TimeoutSeconds      int    `yaml:"timeout_seconds"`
	EnableDynamicDomain bool   `yaml:"enable_dynamic_domain"`
	Token               string `yaml:"token"`
	TokenFile           string `yaml:"token_file"`
	ServerURL           string `yaml:"server_url"`
	DeviceID            string `yaml:"device_id"`
}

type Polygon struct {
	APIKey             string `yaml:"api_key"`
	BaseURL            string `yaml:"base_url"`
	FlatFilesBaseURL   string `yaml:"flat_files_base_url"`
	FlatFilesTool      string `yaml:"flat_files_tool"`
	FlatFilesCacheDir  string `yaml:"flat_files_cache_dir"`
	FlatFilesAccessKey string `yaml:"flat_files_access_key"`
	FlatFilesSecretKey string `yaml:"flat_files_secret_key"`
	TimeoutSeconds     int    `yaml:"timeout_seconds"`
	Trace              bool   `yaml:"trace"`
	Pagination         bool   `yaml:"pagination"`
}

type Redis struct {
	Enabled             bool   `yaml:"enabled"`
	Addr                string `yaml:"addr"`
	Password            string `yaml:"password"`
	DB                  int    `yaml:"db"`
	KeyPrefix           string `yaml:"key_prefix"`
	DialTimeoutSeconds  int    `yaml:"dial_timeout_seconds"`
	ReadTimeoutSeconds  int    `yaml:"read_timeout_seconds"`
	WriteTimeoutSeconds int    `yaml:"write_timeout_seconds"`
}

func DefaultRuntime() Runtime {
	return Runtime{
		ClickHouse: ClickHouse{
			DSN: defaultClickHouseDSN,
		},
		APIServer: APIServer{
			ListenAddr:               defaultListenAddr,
			ReadHeaderTimeoutSeconds: 10,
			ReadTimeoutSeconds:       30,
			WriteTimeoutSeconds:      60,
			IdleTimeoutSeconds:       120,
		},
		API: API{
			RateLimitRPS: 50,
		},
		Paths: Paths{
			SchemaDir: defaultSchemaDir,
		},
		Deribit: Deribit{
			BaseURL: defaultDeribitBaseURL,
		},
		Tiger: Tiger{
			EnableDynamicDomain: true,
		},
		Polygon: Polygon{
			BaseURL:          "https://api.massive.com",
			FlatFilesBaseURL: "https://files.massive.com/flatfiles",
			FlatFilesTool:    "mc",
			TimeoutSeconds:   defaultPolygonTimeoutSec,
			Pagination:       true,
		},
		Redis: Redis{
			Addr:                defaultRedisAddr,
			KeyPrefix:           defaultRedisKeyPrefix,
			DialTimeoutSeconds:  2,
			ReadTimeoutSeconds:  2,
			WriteTimeoutSeconds: 2,
		},
	}
}

func LoadRuntime() (Runtime, error) {
	path := strings.TrimSpace(os.Getenv(EnvConfigPath))
	if path == "" {
		path = defaultConfigPath
	}
	return LoadRuntimeFromPath(path)
}

func LoadRuntimeFromPath(path string) (Runtime, error) {
	cfg := DefaultRuntime()
	cleanPath := strings.TrimSpace(path)
	if cleanPath != "" {
		data, err := os.ReadFile(cleanPath)
		if err != nil {
			if !os.IsNotExist(err) {
				return Runtime{}, fmt.Errorf("read runtime config %s: %w", cleanPath, err)
			}
		} else if err := yaml.Unmarshal(data, &cfg); err != nil {
			return Runtime{}, fmt.Errorf("parse runtime config %s: %w", cleanPath, err)
		}
	}
	cfg.applyEnvOverrides()
	cfg.normalize()
	if err := cfg.Validate(); err != nil {
		return Runtime{}, err
	}
	return cfg, nil
}

func (c *Runtime) applyEnvOverrides() {
	if c == nil {
		return
	}
	if value := strings.TrimSpace(os.Getenv(EnvClickHouseDSN)); value != "" {
		c.ClickHouse.DSN = value
	}
	if value := strings.TrimSpace(os.Getenv(EnvListenAddr)); value != "" {
		c.APIServer.ListenAddr = value
	}
	if value := strings.TrimSpace(os.Getenv(EnvCORSOrigins)); value != "" {
		c.API.CORSOrigins = splitCSV(value)
	}
	if value := strings.TrimSpace(os.Getenv(EnvAPIKeys)); value != "" {
		c.API.APIKeys = splitCSV(value)
	}
	if value := strings.TrimSpace(os.Getenv(EnvRateLimitRPS)); value != "" {
		if parsed, err := strconv.ParseFloat(value, 64); err == nil {
			c.API.RateLimitRPS = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv(EnvSchemaDir)); value != "" {
		c.Paths.SchemaDir = value
	}
	if value := strings.TrimSpace(os.Getenv(EnvDeribitBaseURL)); value != "" {
		c.Deribit.BaseURL = value
	}
	if value := strings.TrimSpace(os.Getenv(EnvTigerID)); value != "" {
		c.Tiger.TigerID = value
	}
	if value := strings.TrimSpace(os.Getenv(EnvTigerPrivateKey)); value != "" {
		c.Tiger.PrivateKey = value
	}
	if value := strings.TrimSpace(os.Getenv(EnvTigerAccount)); value != "" {
		c.Tiger.Account = value
	}
	if value := strings.TrimSpace(os.Getenv(EnvTigerLicense)); value != "" {
		c.Tiger.License = value
	}
	if value := strings.TrimSpace(os.Getenv(EnvTigerEnvironment)); value != "" {
		c.Tiger.Environment = value
	}
	if value := strings.TrimSpace(os.Getenv(EnvTigerLanguage)); value != "" {
		c.Tiger.Language = value
	}
	if value := strings.TrimSpace(os.Getenv(EnvTigerTimezone)); value != "" {
		c.Tiger.Timezone = value
	}
	if value := strings.TrimSpace(os.Getenv(EnvTigerTimeoutSec)); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			c.Tiger.TimeoutSeconds = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv(EnvTigerDynamicDomain)); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			c.Tiger.EnableDynamicDomain = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv(EnvTigerToken)); value != "" {
		c.Tiger.Token = value
	}
	if value := strings.TrimSpace(os.Getenv(EnvTigerTokenFile)); value != "" {
		c.Tiger.TokenFile = value
	}
	if value := strings.TrimSpace(os.Getenv(EnvTigerServerURL)); value != "" {
		c.Tiger.ServerURL = value
	}
	if value := strings.TrimSpace(os.Getenv(EnvTigerDeviceID)); value != "" {
		c.Tiger.DeviceID = value
	}
	if value := strings.TrimSpace(os.Getenv(EnvRedisEnabled)); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			c.Redis.Enabled = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv(EnvRedisAddr)); value != "" {
		c.Redis.Addr = value
	}
	if value := os.Getenv(EnvRedisPassword); value != "" {
		c.Redis.Password = value
	}
	if value := strings.TrimSpace(os.Getenv(EnvRedisDB)); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			c.Redis.DB = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv(EnvRedisKeyPrefix)); value != "" {
		c.Redis.KeyPrefix = value
	}
	if value := strings.TrimSpace(os.Getenv(EnvRedisDialTimeoutSec)); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			c.Redis.DialTimeoutSeconds = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv(EnvRedisReadTimeoutSec)); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			c.Redis.ReadTimeoutSeconds = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv(EnvRedisWriteTimeoutSec)); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			c.Redis.WriteTimeoutSeconds = parsed
		}
	}
}

func (c *Runtime) normalize() {
	if c == nil {
		return
	}
	if strings.TrimSpace(c.ClickHouse.DSN) == "" {
		c.ClickHouse.DSN = defaultClickHouseDSN
	}
	if strings.TrimSpace(c.APIServer.ListenAddr) == "" {
		c.APIServer.ListenAddr = defaultListenAddr
	}
	if c.APIServer.ReadHeaderTimeoutSeconds <= 0 {
		c.APIServer.ReadHeaderTimeoutSeconds = 10
	}
	if c.APIServer.ReadTimeoutSeconds <= 0 {
		c.APIServer.ReadTimeoutSeconds = 30
	}
	if c.APIServer.WriteTimeoutSeconds <= 0 {
		c.APIServer.WriteTimeoutSeconds = 60
	}
	if c.APIServer.IdleTimeoutSeconds <= 0 {
		c.APIServer.IdleTimeoutSeconds = 120
	}
	c.API.CORSOrigins = normalizeCSVList(c.API.CORSOrigins)
	c.API.APIKeys = normalizeCSVList(c.API.APIKeys)
	if c.API.RateLimitRPS <= 0 {
		c.API.RateLimitRPS = 50
	}
	if strings.TrimSpace(c.Paths.SchemaDir) == "" {
		c.Paths.SchemaDir = defaultSchemaDir
	}
	c.Paths.SchemaDir = filepath.Clean(c.Paths.SchemaDir)
	if strings.TrimSpace(c.Deribit.BaseURL) == "" {
		c.Deribit.BaseURL = defaultDeribitBaseURL
	}
	c.Tiger.TigerID = strings.TrimSpace(c.Tiger.TigerID)
	c.Tiger.PrivateKey = strings.TrimSpace(c.Tiger.PrivateKey)
	c.Tiger.Account = strings.TrimSpace(c.Tiger.Account)
	c.Tiger.License = strings.TrimSpace(c.Tiger.License)
	c.Tiger.Environment = strings.ToUpper(strings.TrimSpace(c.Tiger.Environment))
	c.Tiger.Language = strings.TrimSpace(c.Tiger.Language)
	c.Tiger.Timezone = strings.TrimSpace(c.Tiger.Timezone)
	c.Tiger.Token = strings.TrimSpace(c.Tiger.Token)
	c.Tiger.TokenFile = strings.TrimSpace(c.Tiger.TokenFile)
	c.Tiger.ServerURL = strings.TrimSpace(c.Tiger.ServerURL)
	c.Tiger.DeviceID = strings.TrimSpace(c.Tiger.DeviceID)
	if c.Tiger.TimeoutSeconds < 0 {
		c.Tiger.TimeoutSeconds = 0
	}
	c.Polygon.APIKey = strings.TrimSpace(c.Polygon.APIKey)
	c.Polygon.BaseURL = strings.TrimSpace(c.Polygon.BaseURL)
	c.Polygon.FlatFilesBaseURL = strings.TrimSpace(c.Polygon.FlatFilesBaseURL)
	c.Polygon.FlatFilesTool = strings.ToLower(strings.TrimSpace(c.Polygon.FlatFilesTool))
	if strings.TrimSpace(c.Polygon.FlatFilesCacheDir) != "" && !filepath.IsAbs(c.Polygon.FlatFilesCacheDir) {
		c.Polygon.FlatFilesCacheDir = filepath.Clean(c.Polygon.FlatFilesCacheDir)
	}
	c.Polygon.FlatFilesAccessKey = strings.TrimSpace(c.Polygon.FlatFilesAccessKey)
	c.Polygon.FlatFilesSecretKey = strings.TrimSpace(c.Polygon.FlatFilesSecretKey)
	if c.Polygon.TimeoutSeconds <= 0 {
		c.Polygon.TimeoutSeconds = defaultPolygonTimeoutSec
	}
	if strings.TrimSpace(c.Redis.Addr) == "" {
		c.Redis.Addr = defaultRedisAddr
	}
	if strings.TrimSpace(c.Redis.KeyPrefix) == "" {
		c.Redis.KeyPrefix = defaultRedisKeyPrefix
	}
	if c.Redis.DialTimeoutSeconds <= 0 {
		c.Redis.DialTimeoutSeconds = 2
	}
	if c.Redis.ReadTimeoutSeconds <= 0 {
		c.Redis.ReadTimeoutSeconds = 2
	}
	if c.Redis.WriteTimeoutSeconds <= 0 {
		c.Redis.WriteTimeoutSeconds = 2
	}
}

func (c Runtime) Validate() error {
	if strings.TrimSpace(c.ClickHouse.DSN) == "" {
		return fmt.Errorf("clickhouse.dsn is required")
	}
	if c.API.RateLimitRPS <= 0 {
		return fmt.Errorf("api.rate_limit_rps must be greater than zero")
	}
	if c.Redis.Enabled && strings.TrimSpace(c.Redis.Addr) == "" {
		return fmt.Errorf("redis.addr is required when redis is enabled")
	}
	return nil
}

func (c Runtime) RedisDialTimeout() time.Duration {
	return time.Duration(c.Redis.DialTimeoutSeconds) * time.Second
}

func (c Runtime) RedisReadTimeout() time.Duration {
	return time.Duration(c.Redis.ReadTimeoutSeconds) * time.Second
}

func (c Runtime) RedisWriteTimeout() time.Duration {
	return time.Duration(c.Redis.WriteTimeoutSeconds) * time.Second
}

func (c Runtime) APIServerReadHeaderTimeout() time.Duration {
	return time.Duration(c.APIServer.ReadHeaderTimeoutSeconds) * time.Second
}

func (c Runtime) APIServerReadTimeout() time.Duration {
	return time.Duration(c.APIServer.ReadTimeoutSeconds) * time.Second
}

func (c Runtime) APIServerWriteTimeout() time.Duration {
	return time.Duration(c.APIServer.WriteTimeoutSeconds) * time.Second
}

func (c Runtime) APIServerIdleTimeout() time.Duration {
	return time.Duration(c.APIServer.IdleTimeoutSeconds) * time.Second
}

func (c Runtime) SchemaPathCandidates(fileName string) []string {
	baseDir := strings.TrimSpace(c.Paths.SchemaDir)
	if baseDir == "" {
		baseDir = defaultSchemaDir
	}
	if filepath.IsAbs(baseDir) {
		return []string{filepath.Join(baseDir, fileName)}
	}
	candidates := []string{
		filepath.Join(baseDir, fileName),
		filepath.Join("..", baseDir, fileName),
		filepath.Join("../..", baseDir, fileName),
	}
	return slices.Compact(candidates)
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return strings.Split(raw, ",")
}

func normalizeCSVList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		item := strings.TrimSpace(value)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		normalized = append(normalized, item)
	}
	return normalized
}
