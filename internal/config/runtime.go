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

	"github.com/Cyvadra/toktik/internal/secrets"
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
	EnvFMPAPIKey             = "FMP_API_KEY"
	EnvFMPCacheDir           = "TOKTIK_FMP_CACHE_DIR"
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
	EnvAESKey                = "TOKTIK_AES_KEY"
	defaultConfigPath        = "toktik.yaml"
	defaultClickHouseDSN     = "clickhouse://default:@localhost:9000/default"
	defaultListenAddr        = ":9010"
	defaultSchemaDir         = "schema/clickhouse"
	defaultDeribitBaseURL    = "https://www.deribit.com"
	defaultRedisKeyPrefix    = "toktik"
	defaultRedisAddr         = "127.0.0.1:6379"
	defaultPolygonTimeoutSec = 60
	defaultReportsRoot       = "reports/backtests"
)

type Runtime struct {
	ClickHouse ClickHouse `yaml:"clickhouse"`
	APIServer  APIServer  `yaml:"api_server"`
	API        API        `yaml:"api"`
	Paths      Paths      `yaml:"paths"`
	Deribit    Deribit    `yaml:"deribit"`
	Tiger      Tiger      `yaml:"tiger"`
	Polygon    Polygon    `yaml:"polygon"`
	FMP        FMP        `yaml:"fmp"`
	Redis      Redis      `yaml:"redis"`
	AESKey     string     `yaml:"aes_key"`

	// Secrets is the in-memory secrets manager initialised after config load.
	// Not serialised to YAML.
	Secrets *secrets.Manager `yaml:"-"`
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
	CORSOrigins           []string `yaml:"cors_origins"`
	APIKeys               []string `yaml:"api_keys"`
	RateLimitRPS          float64  `yaml:"rate_limit_rps"`
	TrustedProxies        []string `yaml:"trusted_proxies"`
	RequestTimeoutSeconds int      `yaml:"request_timeout_seconds"`
}

type Paths struct {
	SchemaDir   string `yaml:"schema_dir"`
	ReportsRoot string `yaml:"reports_root"`
}

type Deribit struct {
	BaseURL string `yaml:"base_url"`
}

type Tiger struct {
	TigerID             string `yaml:"tiger_id"`
	Account             string `yaml:"account"`
	License             string `yaml:"license"`
	Environment         string `yaml:"environment"`
	Language            string `yaml:"language"`
	Timezone            string `yaml:"timezone"`
	TimeoutSeconds      int    `yaml:"timeout_seconds"`
	EnableDynamicDomain bool   `yaml:"enable_dynamic_domain"`
	TokenFile           string `yaml:"token_file"`
	ServerURL           string `yaml:"server_url"`
	DeviceID            string `yaml:"device_id"`

	privateKey string
	token      string
}

type Polygon struct {
	BaseURL           string `yaml:"base_url"`
	FlatFilesBaseURL  string `yaml:"flat_files_base_url"`
	FlatFilesTool     string `yaml:"flat_files_tool"`
	FlatFilesCacheDir string `yaml:"flat_files_cache_dir"`
	TimeoutSeconds    int    `yaml:"timeout_seconds"`
	Trace             bool   `yaml:"trace"`
	Pagination        bool   `yaml:"pagination"`

	apiKey             string
	flatFilesAccessKey string
	flatFilesSecretKey string
}

type FMP struct {
	CacheDir string `yaml:"cache_dir"`
	apiKey   string
}

func (t *Tiger) UnmarshalYAML(value *yaml.Node) error {
	type rawTiger struct {
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
	var raw rawTiger
	if err := value.Decode(&raw); err != nil {
		return err
	}
	t.TigerID = raw.TigerID
	t.privateKey = raw.PrivateKey
	t.Account = raw.Account
	t.License = raw.License
	t.Environment = raw.Environment
	t.Language = raw.Language
	t.Timezone = raw.Timezone
	t.TimeoutSeconds = raw.TimeoutSeconds
	t.EnableDynamicDomain = raw.EnableDynamicDomain
	t.token = raw.Token
	t.TokenFile = raw.TokenFile
	t.ServerURL = raw.ServerURL
	t.DeviceID = raw.DeviceID
	return nil
}

func (p *Polygon) UnmarshalYAML(value *yaml.Node) error {
	type rawPolygon struct {
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
	var raw rawPolygon
	if err := value.Decode(&raw); err != nil {
		return err
	}
	p.apiKey = raw.APIKey
	p.BaseURL = raw.BaseURL
	p.FlatFilesBaseURL = raw.FlatFilesBaseURL
	p.FlatFilesTool = raw.FlatFilesTool
	p.FlatFilesCacheDir = raw.FlatFilesCacheDir
	p.flatFilesAccessKey = raw.FlatFilesAccessKey
	p.flatFilesSecretKey = raw.FlatFilesSecretKey
	p.TimeoutSeconds = raw.TimeoutSeconds
	p.Trace = raw.Trace
	p.Pagination = raw.Pagination
	return nil
}

func (f *FMP) UnmarshalYAML(value *yaml.Node) error {
	type rawFMP struct {
		CacheDir string `yaml:"cache_dir"`
		APIKey   string `yaml:"api_key"`
	}
	var raw rawFMP
	if err := value.Decode(&raw); err != nil {
		return err
	}
	f.CacheDir = raw.CacheDir
	f.apiKey = raw.APIKey
	return nil
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
			RateLimitRPS:          50,
			RequestTimeoutSeconds: 30,
		},
		Paths: Paths{
			SchemaDir:   defaultSchemaDir,
			ReportsRoot: defaultReportsRoot,
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
		FMP: FMP{},
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
	if err := cfg.sealCredentials(); err != nil {
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
		c.SetTigerPrivateKey(value)
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
		c.SetTigerToken(value)
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
	if value := strings.TrimSpace(os.Getenv(EnvFMPAPIKey)); value != "" {
		c.SetFMPAPIKey(value)
	}
	if value := strings.TrimSpace(os.Getenv(EnvFMPCacheDir)); value != "" {
		c.FMP.CacheDir = value
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
	if value := strings.TrimSpace(os.Getenv(EnvAESKey)); value != "" {
		c.AESKey = value
	}
}

// sealCredentials initialises the secrets manager and encrypts sensitive
// fields in memory, then zeroes the plaintext copies.
func (c *Runtime) sealCredentials() error {
	mgr, err := secrets.New(c.AESKey)
	if err != nil {
		return fmt.Errorf("init secrets manager: %w", err)
	}
	c.Secrets = mgr

	if c.AESKey == "" {
		return nil // passthrough mode – keep plaintext fields as-is
	}

	seal := func(field, value string) string {
		if value == "" {
			return ""
		}
		mgr.Seal(field, value)
		return ""
	}

	c.Tiger.privateKey = seal("tiger.private_key", c.Tiger.privateKey)
	c.Tiger.token = seal("tiger.token", c.Tiger.token)
	c.Polygon.apiKey = seal("polygon.api_key", c.Polygon.apiKey)
	c.Polygon.flatFilesAccessKey = seal("polygon.flat_files_access_key", c.Polygon.flatFilesAccessKey)
	c.Polygon.flatFilesSecretKey = seal("polygon.flat_files_secret_key", c.Polygon.flatFilesSecretKey)
	c.FMP.apiKey = seal("fmp.api_key", c.FMP.apiKey)
	c.Redis.Password = seal("redis.password", c.Redis.Password)
	c.AESKey = "" // don't retain the key itself
	return nil
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
	c.API.TrustedProxies = normalizeCSVList(c.API.TrustedProxies)
	if c.API.RateLimitRPS <= 0 {
		c.API.RateLimitRPS = 50
	}
	if c.API.RequestTimeoutSeconds <= 0 {
		c.API.RequestTimeoutSeconds = 30
	}
	if strings.TrimSpace(c.Paths.SchemaDir) == "" {
		c.Paths.SchemaDir = defaultSchemaDir
	}
	c.Paths.SchemaDir = filepath.Clean(c.Paths.SchemaDir)
	if strings.TrimSpace(c.Paths.ReportsRoot) == "" {
		c.Paths.ReportsRoot = defaultReportsRoot
	}
	c.Paths.ReportsRoot = filepath.Clean(c.Paths.ReportsRoot)
	if strings.TrimSpace(c.Deribit.BaseURL) == "" {
		c.Deribit.BaseURL = defaultDeribitBaseURL
	}
	c.Tiger.TigerID = strings.TrimSpace(c.Tiger.TigerID)
	c.Tiger.privateKey = strings.TrimSpace(c.Tiger.privateKey)
	c.Tiger.Account = strings.TrimSpace(c.Tiger.Account)
	c.Tiger.License = strings.TrimSpace(c.Tiger.License)
	c.Tiger.Environment = strings.ToUpper(strings.TrimSpace(c.Tiger.Environment))
	c.Tiger.Language = strings.TrimSpace(c.Tiger.Language)
	c.Tiger.Timezone = strings.TrimSpace(c.Tiger.Timezone)
	c.Tiger.token = strings.TrimSpace(c.Tiger.token)
	c.Tiger.TokenFile = strings.TrimSpace(c.Tiger.TokenFile)
	c.Tiger.ServerURL = strings.TrimSpace(c.Tiger.ServerURL)
	c.Tiger.DeviceID = strings.TrimSpace(c.Tiger.DeviceID)
	if c.Tiger.TimeoutSeconds < 0 {
		c.Tiger.TimeoutSeconds = 0
	}
	c.Polygon.apiKey = strings.TrimSpace(c.Polygon.apiKey)
	c.Polygon.BaseURL = strings.TrimSpace(c.Polygon.BaseURL)
	c.Polygon.FlatFilesBaseURL = strings.TrimSpace(c.Polygon.FlatFilesBaseURL)
	c.Polygon.FlatFilesTool = strings.ToLower(strings.TrimSpace(c.Polygon.FlatFilesTool))
	if strings.TrimSpace(c.Polygon.FlatFilesCacheDir) != "" && !filepath.IsAbs(c.Polygon.FlatFilesCacheDir) {
		c.Polygon.FlatFilesCacheDir = filepath.Clean(c.Polygon.FlatFilesCacheDir)
	}
	c.Polygon.flatFilesAccessKey = strings.TrimSpace(c.Polygon.flatFilesAccessKey)
	c.Polygon.flatFilesSecretKey = strings.TrimSpace(c.Polygon.flatFilesSecretKey)
	if c.Polygon.TimeoutSeconds <= 0 {
		c.Polygon.TimeoutSeconds = defaultPolygonTimeoutSec
	}
	c.FMP.apiKey = strings.TrimSpace(c.FMP.apiKey)
	if strings.TrimSpace(c.FMP.CacheDir) != "" {
		if filepath.IsAbs(c.FMP.CacheDir) {
			c.FMP.CacheDir = filepath.Clean(c.FMP.CacheDir)
		} else {
			c.FMP.CacheDir = filepath.Clean(c.FMP.CacheDir)
		}
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

func (c Runtime) TigerPrivateKey() (string, error) {
	return c.secretValue("tiger.private_key", c.Tiger.privateKey)
}

func (c Runtime) TigerToken() (string, error) {
	return c.secretValue("tiger.token", c.Tiger.token)
}

func (c Runtime) PolygonAPIKey() (string, error) {
	return c.secretValue("polygon.api_key", c.Polygon.apiKey)
}

func (c Runtime) PolygonFlatFilesAccessKey() (string, error) {
	return c.secretValue("polygon.flat_files_access_key", c.Polygon.flatFilesAccessKey)
}

func (c Runtime) PolygonFlatFilesSecretKey() (string, error) {
	return c.secretValue("polygon.flat_files_secret_key", c.Polygon.flatFilesSecretKey)
}

func (c Runtime) FMPAPIKey() (string, error) {
	return c.secretValue("fmp.api_key", c.FMP.apiKey)
}

func (c *Runtime) SetTigerPrivateKey(value string) {
	if c == nil {
		return
	}
	c.setSecretValue("tiger.private_key", &c.Tiger.privateKey, value)
}

func (c *Runtime) SetTigerToken(value string) {
	if c == nil {
		return
	}
	c.setSecretValue("tiger.token", &c.Tiger.token, value)
}

func (c *Runtime) SetPolygonAPIKey(value string) {
	if c == nil {
		return
	}
	c.setSecretValue("polygon.api_key", &c.Polygon.apiKey, value)
}

func (c *Runtime) SetPolygonFlatFilesAccessKey(value string) {
	if c == nil {
		return
	}
	c.setSecretValue("polygon.flat_files_access_key", &c.Polygon.flatFilesAccessKey, value)
}

func (c *Runtime) SetPolygonFlatFilesSecretKey(value string) {
	if c == nil {
		return
	}
	c.setSecretValue("polygon.flat_files_secret_key", &c.Polygon.flatFilesSecretKey, value)
}

func (c *Runtime) SetFMPAPIKey(value string) {
	if c == nil {
		return
	}
	c.setSecretValue("fmp.api_key", &c.FMP.apiKey, value)
}

func (c Runtime) secretValue(field, fallback string) (string, error) {
	if value := strings.TrimSpace(fallback); value != "" {
		return value, nil
	}
	if c.Secrets == nil {
		return "", nil
	}
	value, err := c.Secrets.Open(field)
	if err != nil {
		return "", fmt.Errorf("load %s from runtime secrets: %w", field, err)
	}
	return strings.TrimSpace(value), nil
}

func (c *Runtime) setSecretValue(field string, target *string, value string) {
	if target == nil {
		return
	}
	trimmed := strings.TrimSpace(value)
	if c.Secrets == nil {
		*target = trimmed
		return
	}
	c.Secrets.Seal(field, trimmed)
	*target = ""
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

// APIRequestTimeout returns the per-request handler timeout.
func (c Runtime) APIRequestTimeout() time.Duration {
	return time.Duration(c.API.RequestTimeoutSeconds) * time.Second
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
