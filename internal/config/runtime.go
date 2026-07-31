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
	EnvConfigPath                            = "TOKTIK_CONFIG"
	EnvClickHouseDSN                         = "CLICKHOUSE_DSN"
	EnvClickHousePriorityEnabled             = "TOKTIK_CLICKHOUSE_PRIORITY_ENABLED"
	EnvClickHousePriorityMaxQueries          = "TOKTIK_CLICKHOUSE_PRIORITY_MAX_CONCURRENT_QUERIES"
	EnvClickHousePriorityMaxThreads          = "TOKTIK_CLICKHOUSE_PRIORITY_MAX_CONCURRENT_THREADS"
	EnvClickHousePriorityBackgroundQueries   = "TOKTIK_CLICKHOUSE_PRIORITY_BACKGROUND_QUERIES"
	EnvClickHousePriorityBackgroundThreads   = "TOKTIK_CLICKHOUSE_PRIORITY_BACKGROUND_THREADS"
	EnvMySQLDSN                              = "MYSQL_DSN"
	EnvMySQLHost                             = "MYSQL_HOST"
	EnvMySQLUser                             = "MYSQL_USER"
	EnvMySQLPassword                         = "MYSQL_PASSWORD"
	EnvMySQLDatabase                         = "MYSQL_DATABASE"
	EnvListenAddr                            = "LISTEN_ADDR"
	EnvCORSOrigins                           = "CORS_ORIGINS"
	EnvRateLimitRPS                          = "RATE_LIMIT_RPS"
	EnvBypassAuthForLocalClients             = "TOKTIK_BYPASS_AUTH_FOR_LOCAL_CLIENTS"
	EnvAPIEnvironment                        = "TOKTIK_API_ENVIRONMENT"
	EnvAPITrafficEnabled                     = "TOKTIK_API_TRAFFIC_ENABLED"
	EnvAPITrafficFlushSeconds                = "TOKTIK_API_TRAFFIC_FLUSH_SECONDS"
	EnvSchemaDir                             = "TOKTIK_SCHEMA_DIR"
	EnvDeribitBaseURL                        = "DERIBIT_BASE_URL"
	EnvFMPAPIKey                             = "FMP_API_KEY"
	EnvFMPCacheDir                           = "TOKTIK_FMP_CACHE_DIR"
	EnvTigerID                               = "TIGEROPEN_TIGER_ID"
	EnvTigerPrivateKey                       = "TIGEROPEN_PRIVATE_KEY"
	EnvTigerAccount                          = "TIGEROPEN_ACCOUNT"
	EnvTigerLicense                          = "TIGEROPEN_LICENSE"
	EnvTigerEnvironment                      = "TIGEROPEN_ENV"
	EnvTigerLanguage                         = "TIGEROPEN_LANGUAGE"
	EnvTigerTimezone                         = "TIGEROPEN_TIMEZONE"
	EnvTigerTimeoutSec                       = "TIGEROPEN_TIMEOUT_SECONDS"
	EnvTigerDynamicDomain                    = "TIGEROPEN_ENABLE_DYNAMIC_DOMAIN"
	EnvTigerToken                            = "TIGEROPEN_TOKEN"
	EnvTigerTokenFile                        = "TIGEROPEN_TOKEN_FILE"
	EnvTigerServerURL                        = "TIGEROPEN_SERVER_URL"
	EnvTigerDeviceID                         = "TIGEROPEN_DEVICE_ID"
	EnvRedisEnabled                          = "TOKTIK_REDIS_ENABLED"
	EnvRedisAddr                             = "TOKTIK_REDIS_ADDR"
	EnvRedisPassword                         = "TOKTIK_REDIS_PASSWORD"
	EnvRedisDB                               = "TOKTIK_REDIS_DB"
	EnvRedisKeyPrefix                        = "TOKTIK_REDIS_KEY_PREFIX"
	EnvRedisDialTimeoutSec                   = "TOKTIK_REDIS_DIAL_TIMEOUT_SECONDS"
	EnvRedisReadTimeoutSec                   = "TOKTIK_REDIS_READ_TIMEOUT_SECONDS"
	EnvRedisWriteTimeoutSec                  = "TOKTIK_REDIS_WRITE_TIMEOUT_SECONDS"
	EnvAPIWarmupRefreshHours                 = "TOKTIK_API_WARMUP_REFRESH_INTERVAL_HOURS"
	EnvAPIWarmupCooldownHours                = "TOKTIK_API_WARMUP_COOLDOWN_HOURS"
	EnvLatestMarketDataEnabled               = "TOKTIK_LATEST_MARKET_DATA_ENABLED"
	EnvLatestMarketDataRedisTTLHours         = "TOKTIK_LATEST_MARKET_DATA_REDIS_TTL_HOURS"
	EnvLatestMarketDataOpenRefreshMinutes    = "TOKTIK_LATEST_MARKET_DATA_OPEN_REFRESH_MINUTES"
	EnvLatestMarketDataClosedRefreshMinutes  = "TOKTIK_LATEST_MARKET_DATA_CLOSED_REFRESH_MINUTES"
	EnvLatestMarketDataStaleAlertHours       = "TOKTIK_LATEST_MARKET_DATA_STALE_ALERT_HOURS"
	EnvLatestMarketDataRefreshTimeoutMinutes = "TOKTIK_LATEST_MARKET_DATA_REFRESH_TIMEOUT_MINUTES"
	EnvLatestMarketDataWorkers               = "TOKTIK_LATEST_MARKET_DATA_WORKERS"
	EnvLatestMarketDataAlwaysRefreshSymbols  = "TOKTIK_LATEST_MARKET_DATA_ALWAYS_REFRESH_SYMBOLS"
	EnvAESKey                                = "TOKTIK_AES_KEY"
	defaultConfigPath                        = "toktik.yaml"
	defaultClickHouseDSN                     = "clickhouse://default:@localhost:9000/default"
	defaultMySQLHost                         = "127.0.0.1:3306"
	defaultMySQLUser                         = "toktik"
	defaultMySQLDatabase                     = "toktik"
	defaultListenAddr                        = ":9010"
	defaultSchemaDir                         = "schema/clickhouse"
	defaultDeribitBaseURL                    = "https://www.deribit.com"
	defaultRedisKeyPrefix                    = "toktik"
	defaultRedisAddr                         = "127.0.0.1:6379"
	defaultPolygonTimeoutSec                 = 60
	defaultReportsRoot                       = "reports/backtests"
	defaultUniverseRebuildStartDate          = "2022-05-01"
	maxLatestMarketDataOptionChainLimit      = 250
)

var defaultLatestMarketDataAlwaysRefreshSymbols = []string{
	"SPY", "QQQ", "AAPL", "NVDA", "HYG", "UUP", "CRED",
	"IBIT", "TLT", "USO", "VTI", "DIA",
	"VGK", "EWU", "EWJ", "EWH", "FXI", "EWA", "EWZ",
	"BE", "SMCI", "CRM", "IBM", "JPM", "ETHU", "ETHA", "MSTR", "ASHR", "MCHI", "KWEB", "VIX",
	"VXX", "UVXY", "SVXY", "SVIX", "UVIX", "VIXY", "VIXM", "VXZ",
	"SHOP", "MELI", "BRK.B", "SPOT", "NET", "SE", "BAC", "OXY", "TME", "TEM", "MCO", "PDD", "CRCL", "MCD", "CRWD",
	"PANW", "VST",
}

type Runtime struct {
	ClickHouse       ClickHouse       `yaml:"clickhouse"`
	MySQL            MySQL            `yaml:"mysql"`
	APIServer        APIServer        `yaml:"api_server"`
	API              API              `yaml:"api"`
	Paths            Paths            `yaml:"paths"`
	Deribit          Deribit          `yaml:"deribit"`
	Tiger            Tiger            `yaml:"tiger"`
	Polygon          Polygon          `yaml:"polygon"`
	FMP              FMP              `yaml:"fmp"`
	LatestMarketData LatestMarketData `yaml:"latest_market_data"`
	Universe         Universe         `yaml:"universe"`
	Redis            Redis            `yaml:"redis"`
	AESKey           string           `yaml:"aes_key"`

	// Secrets is the in-memory secrets manager initialised after config load.
	// Not serialised to YAML.
	Secrets *secrets.Manager `yaml:"-"`
}

type ClickHouse struct {
	DSN      string             `yaml:"dsn"`
	Priority ClickHousePriority `yaml:"priority"`
}

type ClickHousePriority struct {
	Enabled              bool `yaml:"enabled"`
	MaxConcurrentQueries int  `yaml:"max_concurrent_queries"`
	MaxConcurrentThreads int  `yaml:"max_concurrent_threads"`
	BackgroundQueries    int  `yaml:"background_queries"`
	BackgroundThreads    int  `yaml:"background_threads"`
}

type MySQL struct {
	DSN      string `yaml:"dsn"`
	Host     string `yaml:"host"`
	User     string `yaml:"user"`
	Database string `yaml:"database"`
	password string
}

type APIServer struct {
	ListenAddr                 string `yaml:"listen_addr"`
	ReadHeaderTimeoutSeconds   int    `yaml:"read_header_timeout_seconds"`
	ReadTimeoutSeconds         int    `yaml:"read_timeout_seconds"`
	WriteTimeoutSeconds        int    `yaml:"write_timeout_seconds"`
	IdleTimeoutSeconds         int    `yaml:"idle_timeout_seconds"`
	WarmupRefreshIntervalHours int    `yaml:"warmup_refresh_interval_hours"`
	WarmupCooldownHours        int    `yaml:"warmup_cooldown_hours"`
}

type API struct {
	CORSOrigins               []string `yaml:"cors_origins"`
	RateLimitRPS              float64  `yaml:"rate_limit_rps"`
	TrustedProxies            []string `yaml:"trusted_proxies"`
	BypassAuthForLocalClients bool     `yaml:"bypass_auth_for_local_clients"`
	RequestTimeoutSeconds     int      `yaml:"request_timeout_seconds"`
	Environment               string   `yaml:"environment"`
	TrafficEnabled            bool     `yaml:"traffic_enabled"`
	TrafficFlushSeconds       int      `yaml:"traffic_flush_seconds"`
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
	BaseURL           string  `yaml:"base_url"`
	FlatFilesBaseURL  string  `yaml:"flat_files_base_url"`
	FlatFilesTool     string  `yaml:"flat_files_tool"`
	FlatFilesCacheDir string  `yaml:"flat_files_cache_dir"`
	TimeoutSeconds    int     `yaml:"timeout_seconds"`
	Trace             bool    `yaml:"trace"`
	Pagination        bool    `yaml:"pagination"`
	RESTQPS           float64 `yaml:"rest_qps"`
	RESTBurst         int     `yaml:"rest_burst"`
	RetryAttempts     int     `yaml:"retry_attempts"`
	RetryBaseDelayMS  int     `yaml:"retry_base_delay_ms"`
	RetryMaxDelayMS   int     `yaml:"retry_max_delay_ms"`

	apiKey             string
	flatFilesAccessKey string
	flatFilesSecretKey string
}

type FMP struct {
	CacheDir string `yaml:"cache_dir"`
	apiKey   string
}

type LatestMarketData struct {
	Enabled                      bool     `yaml:"enabled"`
	RedisTTLHours                int      `yaml:"redis_ttl_hours"`
	OpenRefreshIntervalMinutes   int      `yaml:"open_refresh_interval_minutes"`
	ClosedRefreshIntervalMinutes int      `yaml:"closed_refresh_interval_minutes"`
	StaleAlertAfterHours         int      `yaml:"stale_alert_after_hours"`
	RefreshTimeoutMinutes        int      `yaml:"refresh_timeout_minutes"`
	Workers                      int      `yaml:"workers"`
	StockProvider                string   `yaml:"stock_provider"`
	OptionProvider               string   `yaml:"option_provider"`
	SmokeSymbols                 []string `yaml:"smoke_symbols"`
	AlwaysRefreshSymbols         []string `yaml:"always_refresh_symbols"`
	OptionChainLimit             int      `yaml:"option_chain_limit"`
	OptionAggregateLimit         int      `yaml:"option_aggregate_limit"`
}

func (m *MySQL) UnmarshalYAML(value *yaml.Node) error {
	type rawMySQL struct {
		DSN      string `yaml:"dsn"`
		Host     string `yaml:"host"`
		User     string `yaml:"user"`
		Password string `yaml:"password"`
		Database string `yaml:"database"`
	}
	var raw rawMySQL
	if err := value.Decode(&raw); err != nil {
		return err
	}
	m.DSN = raw.DSN
	m.Host = raw.Host
	m.User = raw.User
	m.password = raw.Password
	m.Database = raw.Database
	return nil
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
		APIKey             string  `yaml:"api_key"`
		BaseURL            string  `yaml:"base_url"`
		FlatFilesBaseURL   string  `yaml:"flat_files_base_url"`
		FlatFilesTool      string  `yaml:"flat_files_tool"`
		FlatFilesCacheDir  string  `yaml:"flat_files_cache_dir"`
		FlatFilesAccessKey string  `yaml:"flat_files_access_key"`
		FlatFilesSecretKey string  `yaml:"flat_files_secret_key"`
		TimeoutSeconds     int     `yaml:"timeout_seconds"`
		Trace              bool    `yaml:"trace"`
		Pagination         bool    `yaml:"pagination"`
		RESTQPS            float64 `yaml:"rest_qps"`
		RESTBurst          int     `yaml:"rest_burst"`
		RetryAttempts      int     `yaml:"retry_attempts"`
		RetryBaseDelayMS   int     `yaml:"retry_base_delay_ms"`
		RetryMaxDelayMS    int     `yaml:"retry_max_delay_ms"`
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
	p.RESTQPS = raw.RESTQPS
	p.RESTBurst = raw.RESTBurst
	p.RetryAttempts = raw.RetryAttempts
	p.RetryBaseDelayMS = raw.RetryBaseDelayMS
	p.RetryMaxDelayMS = raw.RetryMaxDelayMS
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

// Universe configures server-owned rebuild policy for named universes.
// The rebuild range end is derived from reference market data at runtime.
type Universe struct {
	RebuildStartDate string `yaml:"rebuild_start_date"`
}

func DefaultRuntime() Runtime {
	return Runtime{
		ClickHouse: ClickHouse{
			DSN: defaultClickHouseDSN,
		},
		MySQL: MySQL{
			Host:     defaultMySQLHost,
			User:     defaultMySQLUser,
			Database: defaultMySQLDatabase,
		},
		APIServer: APIServer{
			ListenAddr:                 defaultListenAddr,
			ReadHeaderTimeoutSeconds:   10,
			ReadTimeoutSeconds:         30,
			WriteTimeoutSeconds:        180,
			IdleTimeoutSeconds:         120,
			WarmupRefreshIntervalHours: 22,
			WarmupCooldownHours:        20,
		},
		API: API{
			RateLimitRPS:          50,
			RequestTimeoutSeconds: 180,
			TrafficEnabled:        true,
			TrafficFlushSeconds:   15,
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
			RESTQPS:          4,
			RESTBurst:        1,
			RetryAttempts:    4,
			RetryBaseDelayMS: 500,
			RetryMaxDelayMS:  8000,
		},
		FMP: FMP{},
		LatestMarketData: LatestMarketData{
			Enabled:                      false,
			RedisTTLHours:                72,
			OpenRefreshIntervalMinutes:   60,
			ClosedRefreshIntervalMinutes: 180,
			StaleAlertAfterHours:         6,
			RefreshTimeoutMinutes:        15,
			Workers:                      4,
			StockProvider:                "fmp",
			OptionProvider:               "polygon",
			SmokeSymbols:                 []string{"SPY", "AAPL"},
			AlwaysRefreshSymbols:         slices.Clone(defaultLatestMarketDataAlwaysRefreshSymbols),
			OptionChainLimit:             maxLatestMarketDataOptionChainLimit,
			OptionAggregateLimit:         50,
		},
		Universe: Universe{
			RebuildStartDate: defaultUniverseRebuildStartDate,
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
	if value := strings.TrimSpace(os.Getenv(EnvClickHousePriorityEnabled)); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			c.ClickHouse.Priority.Enabled = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv(EnvClickHousePriorityMaxQueries)); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			c.ClickHouse.Priority.MaxConcurrentQueries = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv(EnvClickHousePriorityMaxThreads)); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			c.ClickHouse.Priority.MaxConcurrentThreads = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv(EnvClickHousePriorityBackgroundQueries)); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			c.ClickHouse.Priority.BackgroundQueries = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv(EnvClickHousePriorityBackgroundThreads)); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			c.ClickHouse.Priority.BackgroundThreads = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv(EnvMySQLDSN)); value != "" {
		c.MySQL.DSN = value
	}
	if value := strings.TrimSpace(os.Getenv(EnvMySQLHost)); value != "" {
		c.MySQL.Host = value
	}
	if value := strings.TrimSpace(os.Getenv(EnvMySQLUser)); value != "" {
		c.MySQL.User = value
	}
	if value := os.Getenv(EnvMySQLPassword); value != "" {
		c.SetMySQLPassword(value)
	}
	if value := strings.TrimSpace(os.Getenv(EnvMySQLDatabase)); value != "" {
		c.MySQL.Database = value
	}
	if value := strings.TrimSpace(os.Getenv(EnvListenAddr)); value != "" {
		c.APIServer.ListenAddr = value
	}
	if value := strings.TrimSpace(os.Getenv(EnvCORSOrigins)); value != "" {
		c.API.CORSOrigins = splitCSV(value)
	}
	if value := strings.TrimSpace(os.Getenv(EnvRateLimitRPS)); value != "" {
		if parsed, err := strconv.ParseFloat(value, 64); err == nil {
			c.API.RateLimitRPS = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv(EnvBypassAuthForLocalClients)); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			c.API.BypassAuthForLocalClients = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv(EnvAPIEnvironment)); value != "" {
		c.API.Environment = value
	}
	if value := strings.TrimSpace(os.Getenv(EnvAPITrafficEnabled)); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			c.API.TrafficEnabled = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv(EnvAPITrafficFlushSeconds)); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			c.API.TrafficFlushSeconds = parsed
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
	if value := strings.TrimSpace(os.Getenv(EnvAPIWarmupRefreshHours)); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			c.APIServer.WarmupRefreshIntervalHours = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv(EnvAPIWarmupCooldownHours)); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			c.APIServer.WarmupCooldownHours = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv(EnvLatestMarketDataEnabled)); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			c.LatestMarketData.Enabled = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv(EnvLatestMarketDataRedisTTLHours)); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			c.LatestMarketData.RedisTTLHours = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv(EnvLatestMarketDataOpenRefreshMinutes)); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			c.LatestMarketData.OpenRefreshIntervalMinutes = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv(EnvLatestMarketDataClosedRefreshMinutes)); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			c.LatestMarketData.ClosedRefreshIntervalMinutes = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv(EnvLatestMarketDataStaleAlertHours)); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			c.LatestMarketData.StaleAlertAfterHours = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv(EnvLatestMarketDataRefreshTimeoutMinutes)); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			c.LatestMarketData.RefreshTimeoutMinutes = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv(EnvLatestMarketDataWorkers)); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			c.LatestMarketData.Workers = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv(EnvLatestMarketDataAlwaysRefreshSymbols)); value != "" {
		c.LatestMarketData.AlwaysRefreshSymbols = splitCSV(value)
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
	c.MySQL.password = seal("mysql.password", c.MySQL.password)
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
	c.MySQL.DSN = strings.TrimSpace(c.MySQL.DSN)
	c.MySQL.Host = strings.TrimSpace(c.MySQL.Host)
	c.MySQL.User = strings.TrimSpace(c.MySQL.User)
	c.MySQL.Database = strings.TrimSpace(c.MySQL.Database)
	c.MySQL.password = strings.TrimSpace(c.MySQL.password)
	if c.MySQL.Host == "" {
		c.MySQL.Host = defaultMySQLHost
	}
	if !strings.Contains(c.MySQL.Host, ":") {
		c.MySQL.Host += ":3306"
	}
	if c.MySQL.User == "" {
		c.MySQL.User = defaultMySQLUser
	}
	if c.MySQL.Database == "" {
		c.MySQL.Database = defaultMySQLDatabase
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
	if c.APIServer.WarmupRefreshIntervalHours <= 0 {
		c.APIServer.WarmupRefreshIntervalHours = 22
	}
	if c.APIServer.WarmupCooldownHours <= 0 {
		c.APIServer.WarmupCooldownHours = 20
	}
	c.API.CORSOrigins = normalizeCSVList(c.API.CORSOrigins)
	c.API.TrustedProxies = normalizeCSVList(c.API.TrustedProxies)
	if c.API.RateLimitRPS <= 0 {
		c.API.RateLimitRPS = 50
	}
	if c.API.RequestTimeoutSeconds <= 0 {
		c.API.RequestTimeoutSeconds = 180
	}
	if c.API.TrafficFlushSeconds <= 0 {
		c.API.TrafficFlushSeconds = 15
	}
	c.API.Environment = strings.ToLower(strings.TrimSpace(c.API.Environment))
	if c.API.Environment == "" {
		c.API.Environment = "prod"
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
	if c.Polygon.RESTQPS <= 0 {
		c.Polygon.RESTQPS = 4
	}
	if c.Polygon.RESTBurst <= 0 {
		c.Polygon.RESTBurst = 1
	}
	if c.Polygon.RetryAttempts <= 0 {
		c.Polygon.RetryAttempts = 4
	}
	if c.Polygon.RetryBaseDelayMS <= 0 {
		c.Polygon.RetryBaseDelayMS = 500
	}
	if c.Polygon.RetryMaxDelayMS <= 0 {
		c.Polygon.RetryMaxDelayMS = 8000
	}
	if c.Polygon.RetryMaxDelayMS < c.Polygon.RetryBaseDelayMS {
		c.Polygon.RetryMaxDelayMS = c.Polygon.RetryBaseDelayMS
	}
	c.FMP.apiKey = strings.TrimSpace(c.FMP.apiKey)
	if strings.TrimSpace(c.FMP.CacheDir) != "" {
		if filepath.IsAbs(c.FMP.CacheDir) {
			c.FMP.CacheDir = filepath.Clean(c.FMP.CacheDir)
		} else {
			c.FMP.CacheDir = filepath.Clean(c.FMP.CacheDir)
		}
	}
	c.LatestMarketData.StockProvider = strings.ToLower(strings.TrimSpace(c.LatestMarketData.StockProvider))
	if c.LatestMarketData.StockProvider == "" {
		c.LatestMarketData.StockProvider = "fmp"
	}
	c.LatestMarketData.OptionProvider = strings.ToLower(strings.TrimSpace(c.LatestMarketData.OptionProvider))
	if c.LatestMarketData.OptionProvider == "" {
		c.LatestMarketData.OptionProvider = "polygon"
	}
	c.LatestMarketData.SmokeSymbols = normalizeCSVList(c.LatestMarketData.SmokeSymbols)
	if len(c.LatestMarketData.SmokeSymbols) == 0 {
		c.LatestMarketData.SmokeSymbols = []string{"SPY", "AAPL"}
	}
	c.LatestMarketData.AlwaysRefreshSymbols = normalizeCSVList(c.LatestMarketData.AlwaysRefreshSymbols)
	if len(c.LatestMarketData.AlwaysRefreshSymbols) == 0 {
		c.LatestMarketData.AlwaysRefreshSymbols = slices.Clone(defaultLatestMarketDataAlwaysRefreshSymbols)
	}
	if c.LatestMarketData.RedisTTLHours <= 0 {
		c.LatestMarketData.RedisTTLHours = 72
	}
	if c.LatestMarketData.OpenRefreshIntervalMinutes <= 0 {
		c.LatestMarketData.OpenRefreshIntervalMinutes = 60
	}
	if c.LatestMarketData.ClosedRefreshIntervalMinutes <= 0 {
		c.LatestMarketData.ClosedRefreshIntervalMinutes = 180
	}
	if c.LatestMarketData.StaleAlertAfterHours <= 0 {
		c.LatestMarketData.StaleAlertAfterHours = 6
	}
	if c.LatestMarketData.RefreshTimeoutMinutes <= 0 {
		c.LatestMarketData.RefreshTimeoutMinutes = 15
	}
	if c.LatestMarketData.Workers <= 0 {
		c.LatestMarketData.Workers = 4
	}
	if c.LatestMarketData.OptionChainLimit <= 0 {
		c.LatestMarketData.OptionChainLimit = maxLatestMarketDataOptionChainLimit
	} else if c.LatestMarketData.OptionChainLimit > maxLatestMarketDataOptionChainLimit {
		c.LatestMarketData.OptionChainLimit = maxLatestMarketDataOptionChainLimit
	}
	if c.LatestMarketData.OptionAggregateLimit <= 0 {
		c.LatestMarketData.OptionAggregateLimit = 50
	}
	c.Universe.RebuildStartDate = strings.TrimSpace(c.Universe.RebuildStartDate)
	if c.Universe.RebuildStartDate == "" {
		c.Universe.RebuildStartDate = defaultUniverseRebuildStartDate
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
	if _, err := time.Parse("2006-01-02", c.Universe.RebuildStartDate); err != nil {
		return fmt.Errorf("universe.rebuild_start_date must use YYYY-MM-DD: %w", err)
	}
	if strings.TrimSpace(c.ClickHouse.DSN) == "" {
		return fmt.Errorf("clickhouse.dsn is required")
	}
	if strings.TrimSpace(c.MySQL.DSN) == "" {
		if strings.TrimSpace(c.MySQL.Host) == "" {
			return fmt.Errorf("mysql.host is required")
		}
		if strings.TrimSpace(c.MySQL.User) == "" {
			return fmt.Errorf("mysql.user is required")
		}
		if strings.TrimSpace(c.MySQL.Database) == "" {
			return fmt.Errorf("mysql.database is required")
		}
	}
	if c.API.RateLimitRPS <= 0 {
		return fmt.Errorf("api.rate_limit_rps must be greater than zero")
	}
	if c.Redis.Enabled && strings.TrimSpace(c.Redis.Addr) == "" {
		return fmt.Errorf("redis.addr is required when redis is enabled")
	}
	if c.LatestMarketData.Enabled && !c.Redis.Enabled {
		return fmt.Errorf("latest_market_data requires redis.enabled=true")
	}
	if c.LatestMarketData.Enabled && c.LatestMarketData.StockProvider != "fmp" && c.LatestMarketData.StockProvider != "polygon" {
		return fmt.Errorf("latest_market_data.stock_provider must be fmp or polygon")
	}
	if c.LatestMarketData.Enabled && c.LatestMarketData.OptionProvider != "polygon" {
		return fmt.Errorf("latest_market_data.option_provider must be polygon")
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

func (c Runtime) MySQLPassword() (string, error) {
	return c.secretValue("mysql.password", c.MySQL.password)
}

func (c Runtime) MySQLDSN() (string, error) {
	if dsn := strings.TrimSpace(c.MySQL.DSN); dsn != "" {
		return dsn, nil
	}
	password, err := c.MySQLPassword()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s:%s@tcp(%s)/%s?charset=utf8mb4&parseTime=True&loc=Local", c.MySQL.User, password, c.MySQL.Host, c.MySQL.Database), nil
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

func (c *Runtime) SetMySQLPassword(value string) {
	if c == nil {
		return
	}
	c.setSecretValue("mysql.password", &c.MySQL.password, value)
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

func (c Runtime) APIServerWarmupRefreshInterval() time.Duration {
	return time.Duration(c.APIServer.WarmupRefreshIntervalHours) * time.Hour
}

func (c Runtime) APIServerWarmupCooldown() time.Duration {
	return time.Duration(c.APIServer.WarmupCooldownHours) * time.Hour
}

func (c Runtime) LatestMarketDataRedisTTL() time.Duration {
	return time.Duration(c.LatestMarketData.RedisTTLHours) * time.Hour
}

func (c Runtime) LatestMarketDataOpenRefreshInterval() time.Duration {
	return time.Duration(c.LatestMarketData.OpenRefreshIntervalMinutes) * time.Minute
}

func (c Runtime) LatestMarketDataClosedRefreshInterval() time.Duration {
	return time.Duration(c.LatestMarketData.ClosedRefreshIntervalMinutes) * time.Minute
}

func (c Runtime) LatestMarketDataStaleAlertAfter() time.Duration {
	return time.Duration(c.LatestMarketData.StaleAlertAfterHours) * time.Hour
}

func (c Runtime) LatestMarketDataRefreshTimeout() time.Duration {
	return time.Duration(c.LatestMarketData.RefreshTimeoutMinutes) * time.Minute
}

// APIRequestTimeout returns the per-request handler timeout.
func (c Runtime) APIRequestTimeout() time.Duration {
	return time.Duration(c.API.RequestTimeoutSeconds) * time.Second
}

// UniverseRebuildStart returns the configured inclusive start date for
// force-refresh universe rebuilds. Runtime validation guarantees parsing.
func (c Runtime) UniverseRebuildStart() time.Time {
	start, _ := time.Parse("2006-01-02", c.Universe.RebuildStartDate)
	return start.UTC()
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
