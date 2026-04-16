package tigerapi

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	runtimeconfig "github.com/Cyvadra/toktik/internal/config"
	tigerconfig "github.com/tigerfintech/openapi-go-sdk/config"
)

const (
	EnvTigerID             = "TIGEROPEN_TIGER_ID"
	EnvPrivateKey          = "TIGEROPEN_PRIVATE_KEY"
	EnvAccount             = "TIGEROPEN_ACCOUNT"
	EnvLicense             = "TIGEROPEN_LICENSE"
	EnvEnvironment         = "TIGEROPEN_ENV"
	EnvLanguage            = "TIGEROPEN_LANGUAGE"
	EnvTimezone            = "TIGEROPEN_TIMEZONE"
	EnvTimeoutSeconds      = "TIGEROPEN_TIMEOUT_SECONDS"
	EnvEnableDynamicDomain = "TIGEROPEN_ENABLE_DYNAMIC_DOMAIN"
	EnvToken               = "TIGEROPEN_TOKEN"
	EnvTokenFile           = "TIGEROPEN_TOKEN_FILE"
	EnvServerURL           = "TIGEROPEN_SERVER_URL"
	defaultTokenFile       = "tiger_openapi_token.properties"
)

type Environment string

const (
	EnvironmentProd    Environment = "PROD"
	EnvironmentSandbox Environment = "SANDBOX"
)

type Config struct {
	TigerID             string
	PrivateKey          string
	Account             string
	License             string
	Environment         Environment
	Language            string
	Timezone            string
	Timeout             time.Duration
	EnableDynamicDomain bool
	Token               string
	TokenFile           string
	ServerURL           string
	DeviceID            string
}

func LoadConfigFromEnv() (Config, error) {
	cfg := Config{
		TigerID:             strings.TrimSpace(os.Getenv(EnvTigerID)),
		PrivateKey:          strings.TrimSpace(os.Getenv(EnvPrivateKey)),
		Account:             strings.TrimSpace(os.Getenv(EnvAccount)),
		License:             strings.TrimSpace(os.Getenv(EnvLicense)),
		Language:            strings.TrimSpace(os.Getenv(EnvLanguage)),
		Timezone:            strings.TrimSpace(os.Getenv(EnvTimezone)),
		Token:               strings.TrimSpace(os.Getenv(EnvToken)),
		TokenFile:           strings.TrimSpace(os.Getenv(EnvTokenFile)),
		ServerURL:           strings.TrimSpace(os.Getenv(EnvServerURL)),
		EnableDynamicDomain: true,
	}

	envValue := strings.ToUpper(strings.TrimSpace(os.Getenv(EnvEnvironment)))
	if envValue != "" {
		cfg.Environment = Environment(envValue)
	}

	if timeoutValue := strings.TrimSpace(os.Getenv(EnvTimeoutSeconds)); timeoutValue != "" {
		seconds, err := strconv.Atoi(timeoutValue)
		if err != nil || seconds <= 0 {
			return Config{}, fmt.Errorf("invalid %s value %q", EnvTimeoutSeconds, timeoutValue)
		}
		cfg.Timeout = time.Duration(seconds) * time.Second
	}

	if dynamicValue := strings.TrimSpace(os.Getenv(EnvEnableDynamicDomain)); dynamicValue != "" {
		enabled, err := strconv.ParseBool(dynamicValue)
		if err != nil {
			return Config{}, fmt.Errorf("invalid %s value %q", EnvEnableDynamicDomain, dynamicValue)
		}
		cfg.EnableDynamicDomain = enabled
	}

	if cfg.Token == "" {
		token, err := loadTokenFromFileIfAvailable(cfg.TokenFile)
		if err != nil {
			return Config{}, err
		}
		cfg.Token = token
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func LoadConfigFromRuntime(runtimeCfg runtimeconfig.Runtime) (Config, error) {
	privateKey, err := runtimeCfg.TigerPrivateKey()
	if err != nil {
		return Config{}, err
	}
	token, err := runtimeCfg.TigerToken()
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		TigerID:             strings.TrimSpace(runtimeCfg.Tiger.TigerID),
		PrivateKey:          strings.TrimSpace(privateKey),
		Account:             strings.TrimSpace(runtimeCfg.Tiger.Account),
		License:             strings.TrimSpace(runtimeCfg.Tiger.License),
		Environment:         Environment(strings.ToUpper(strings.TrimSpace(runtimeCfg.Tiger.Environment))),
		Language:            strings.TrimSpace(runtimeCfg.Tiger.Language),
		Timezone:            strings.TrimSpace(runtimeCfg.Tiger.Timezone),
		Timeout:             time.Duration(runtimeCfg.Tiger.TimeoutSeconds) * time.Second,
		EnableDynamicDomain: runtimeCfg.Tiger.EnableDynamicDomain,
		Token:               strings.TrimSpace(token),
		TokenFile:           strings.TrimSpace(runtimeCfg.Tiger.TokenFile),
		ServerURL:           strings.TrimSpace(runtimeCfg.Tiger.ServerURL),
		DeviceID:            strings.TrimSpace(runtimeCfg.Tiger.DeviceID),
	}

	if cfg.Token == "" {
		token, err := loadTokenFromFileIfAvailable(cfg.TokenFile)
		if err != nil {
			return Config{}, err
		}
		cfg.Token = token
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (c Config) Validate() error {
	missing := make([]string, 0, 5)
	if c.TigerID == "" {
		missing = append(missing, EnvTigerID)
	}
	if c.PrivateKey == "" {
		missing = append(missing, EnvPrivateKey)
	}
	if c.Account == "" {
		missing = append(missing, EnvAccount)
	}
	if c.License == "" {
		missing = append(missing, EnvLicense)
	}
	if strings.TrimSpace(string(c.Environment)) == "" {
		missing = append(missing, EnvEnvironment)
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required tigerapi environment variables: %s", strings.Join(missing, ", "))
	}

	switch c.normalizedEnvironment() {
	case EnvironmentProd, EnvironmentSandbox:
		return nil
	default:
		return fmt.Errorf("unsupported %s value %q", EnvEnvironment, c.Environment)
	}
}

func (c Config) normalizedEnvironment() Environment {
	return Environment(strings.ToUpper(strings.TrimSpace(string(c.Environment))))
}

func (c Config) sandboxDebug() bool {
	return c.normalizedEnvironment() == EnvironmentSandbox
}

func (c Config) toSDKOptions() []tigerconfig.Option {
	opts := []tigerconfig.Option{
		tigerconfig.WithTigerID(c.TigerID),
		tigerconfig.WithPrivateKey(c.PrivateKey),
		tigerconfig.WithAccount(c.Account),
		tigerconfig.WithLicense(c.License),
		tigerconfig.WithSandboxDebug(c.sandboxDebug()),
		tigerconfig.WithEnableDynamicDomain(c.EnableDynamicDomain),
	}
	if c.Language != "" {
		opts = append(opts, tigerconfig.WithLanguage(c.Language))
	}
	if c.Timezone != "" {
		opts = append(opts, tigerconfig.WithTimezone(c.Timezone))
	}
	if c.Timeout > 0 {
		opts = append(opts, tigerconfig.WithTimeout(c.Timeout))
	}
	if c.Token != "" {
		opts = append(opts, tigerconfig.WithToken(c.Token))
	}
	return opts
}

func loadTokenFromFileIfAvailable(explicitPath string) (string, error) {
	tokenFilePath := explicitPath
	if tokenFilePath == "" {
		tokenFilePath = defaultTokenFile
		if _, err := os.Stat(tokenFilePath); err != nil {
			if os.IsNotExist(err) {
				return "", nil
			}
			return "", fmt.Errorf("stat %s: %w", tokenFilePath, err)
		}
	}

	manager := tigerconfig.NewTokenManager(tigerconfig.WithTokenFilePath(tokenFilePath))
	token, err := manager.LoadToken()
	if err != nil {
		if explicitPath == "" && os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("load token from %s: %w", tokenFilePath, err)
	}
	return strings.TrimSpace(token), nil
}
