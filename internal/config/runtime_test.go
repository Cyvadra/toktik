package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadRuntimeFromPathYAML(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "toktik.yaml")
	content := []byte("clickhouse:\n" +
		"  dsn: \"clickhouse://user:pass@clickhouse.internal:9000/quant\"\n" +
		"api_server:\n" +
		"  listen_addr: \":7777\"\n" +
		"api:\n" +
		"  cors_origins:\n" +
		"    - \"https://one.example\"\n" +
		"    - \"https://two.example\"\n" +
		"  api_keys:\n" +
		"    - \"alpha\"\n" +
		"    - \"beta\"\n" +
		"  rate_limit_rps: 125\n" +
		"paths:\n" +
		"  schema_dir: \"/srv/toktik/schema\"\n" +
		"thetadata:\n" +
		"  base_url: \"http://theta.internal:25503/v3\"\n" +
		"  timeout_seconds: 45\n" +
		"deribit:\n" +
		"  base_url: \"https://deribit-proxy.internal\"\n" +
		"tiger:\n" +
		"  tiger_id: \"20100001\"\n" +
		"  private_key: \"runtime-private-key\"\n" +
		"  account: \"acct-1\"\n" +
		"  license: \"TBNZ\"\n" +
		"  environment: \"PROD\"\n" +
		"  language: \"en_US\"\n" +
		"  timezone: \"America/New_York\"\n" +
		"  timeout_seconds: 12\n" +
		"  enable_dynamic_domain: false\n" +
		"  token: \"runtime-token\"\n" +
		"  token_file: \"/tmp/tiger-token.properties\"\n" +
		"  server_url: \"https://tiger-proxy.internal\"\n" +
		"  device_id: \"device-123\"\n")
	if err := os.WriteFile(configPath, content, 0o644); err != nil {
		t.Fatalf("WriteFile(%q) failed: %v", configPath, err)
	}

	cfg, err := LoadRuntimeFromPath(configPath)
	if err != nil {
		t.Fatalf("LoadRuntimeFromPath(%q) failed: %v", configPath, err)
	}

	if cfg.ClickHouse.DSN != "clickhouse://user:pass@clickhouse.internal:9000/quant" {
		t.Fatalf("unexpected clickhouse dsn: %q", cfg.ClickHouse.DSN)
	}
	if cfg.APIServer.ListenAddr != ":7777" {
		t.Fatalf("unexpected api listen addr: %q", cfg.APIServer.ListenAddr)
	}
	if !reflect.DeepEqual(cfg.API.CORSOrigins, []string{"https://one.example", "https://two.example"}) {
		t.Fatalf("unexpected cors origins: %#v", cfg.API.CORSOrigins)
	}
	if !reflect.DeepEqual(cfg.API.APIKeys, []string{"alpha", "beta"}) {
		t.Fatalf("unexpected api keys: %#v", cfg.API.APIKeys)
	}
	if cfg.API.RateLimitRPS != 125 {
		t.Fatalf("unexpected api rate limit: %v", cfg.API.RateLimitRPS)
	}
	if cfg.Paths.SchemaDir != "/srv/toktik/schema" {
		t.Fatalf("unexpected schema dir: %q", cfg.Paths.SchemaDir)
	}
	if cfg.ThetaData.BaseURL != "http://theta.internal:25503/v3" {
		t.Fatalf("unexpected theta base url: %q", cfg.ThetaData.BaseURL)
	}
	if cfg.ThetaData.TimeoutSeconds != 45 {
		t.Fatalf("unexpected theta timeout: %d", cfg.ThetaData.TimeoutSeconds)
	}
	if cfg.Deribit.BaseURL != "https://deribit-proxy.internal" {
		t.Fatalf("unexpected deribit base url: %q", cfg.Deribit.BaseURL)
	}
	if cfg.Tiger.TigerID != "20100001" || cfg.Tiger.Account != "acct-1" {
		t.Fatalf("unexpected tiger identity config: %#v", cfg.Tiger)
	}
	if cfg.Tiger.Environment != "PROD" || cfg.Tiger.Language != "en_US" || cfg.Tiger.Timezone != "America/New_York" {
		t.Fatalf("unexpected tiger runtime config: %#v", cfg.Tiger)
	}
	if cfg.Tiger.TimeoutSeconds != 12 || cfg.Tiger.EnableDynamicDomain {
		t.Fatalf("unexpected tiger timeout or dynamic domain config: %#v", cfg.Tiger)
	}
	if cfg.Tiger.Token != "runtime-token" || cfg.Tiger.TokenFile != "/tmp/tiger-token.properties" || cfg.Tiger.ServerURL != "https://tiger-proxy.internal" || cfg.Tiger.DeviceID != "device-123" {
		t.Fatalf("unexpected tiger auth config: %#v", cfg.Tiger)
	}
}

func TestLoadRuntimeFromPathEnvOverrides(t *testing.T) {
	t.Setenv(EnvClickHouseDSN, "clickhouse://env@clickhouse:9000/envdb")
	t.Setenv(EnvListenAddr, ":9090")
	t.Setenv(EnvCORSOrigins, "https://alpha.example, https://beta.example")
	t.Setenv(EnvAPIKeys, "key-a, key-b")
	t.Setenv(EnvRateLimitRPS, "75")
	t.Setenv(EnvSchemaDir, "/opt/toktik/schema")
	t.Setenv(EnvThetaDataBaseURL, "http://theta-env:25503/v3")
	t.Setenv(EnvThetaDataTimeoutSec, "33")
	t.Setenv(EnvDeribitBaseURL, "https://deribit-env.example")
	t.Setenv(EnvTigerID, "20109999")
	t.Setenv(EnvTigerPrivateKey, "env-private-key")
	t.Setenv(EnvTigerAccount, "env-account")
	t.Setenv(EnvTigerLicense, "TBNZ")
	t.Setenv(EnvTigerEnvironment, "sandbox")
	t.Setenv(EnvTigerLanguage, "zh_CN")
	t.Setenv(EnvTigerTimezone, "Asia/Shanghai")
	t.Setenv(EnvTigerTimeoutSec, "44")
	t.Setenv(EnvTigerDynamicDomain, "false")
	t.Setenv(EnvTigerToken, "env-token")
	t.Setenv(EnvTigerTokenFile, "/tmp/env-tiger-token.properties")
	t.Setenv(EnvTigerServerURL, "https://tiger-env.example")
	t.Setenv(EnvTigerDeviceID, "env-device")

	cfg, err := LoadRuntimeFromPath(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("LoadRuntimeFromPath(missing) failed: %v", err)
	}

	if cfg.ClickHouse.DSN != "clickhouse://env@clickhouse:9000/envdb" {
		t.Fatalf("unexpected clickhouse dsn override: %q", cfg.ClickHouse.DSN)
	}
	if cfg.APIServer.ListenAddr != ":9090" {
		t.Fatalf("unexpected listen addr override: %q", cfg.APIServer.ListenAddr)
	}
	if !reflect.DeepEqual(cfg.API.CORSOrigins, []string{"https://alpha.example", "https://beta.example"}) {
		t.Fatalf("unexpected cors overrides: %#v", cfg.API.CORSOrigins)
	}
	if !reflect.DeepEqual(cfg.API.APIKeys, []string{"key-a", "key-b"}) {
		t.Fatalf("unexpected api key overrides: %#v", cfg.API.APIKeys)
	}
	if cfg.API.RateLimitRPS != 75 {
		t.Fatalf("unexpected rate limit override: %v", cfg.API.RateLimitRPS)
	}
	if cfg.Paths.SchemaDir != "/opt/toktik/schema" {
		t.Fatalf("unexpected schema dir override: %q", cfg.Paths.SchemaDir)
	}
	if cfg.ThetaData.BaseURL != "http://theta-env:25503/v3" {
		t.Fatalf("unexpected theta url override: %q", cfg.ThetaData.BaseURL)
	}
	if cfg.ThetaData.TimeoutSeconds != 33 {
		t.Fatalf("unexpected theta timeout override: %d", cfg.ThetaData.TimeoutSeconds)
	}
	if cfg.Deribit.BaseURL != "https://deribit-env.example" {
		t.Fatalf("unexpected deribit override: %q", cfg.Deribit.BaseURL)
	}
	if cfg.Tiger.TigerID != "20109999" || cfg.Tiger.Account != "env-account" || cfg.Tiger.Environment != "SANDBOX" {
		t.Fatalf("unexpected tiger identity override: %#v", cfg.Tiger)
	}
	if cfg.Tiger.Language != "zh_CN" || cfg.Tiger.Timezone != "Asia/Shanghai" || cfg.Tiger.TimeoutSeconds != 44 {
		t.Fatalf("unexpected tiger runtime override: %#v", cfg.Tiger)
	}
	if cfg.Tiger.EnableDynamicDomain || cfg.Tiger.Token != "env-token" || cfg.Tiger.TokenFile != "/tmp/env-tiger-token.properties" || cfg.Tiger.ServerURL != "https://tiger-env.example" || cfg.Tiger.DeviceID != "env-device" {
		t.Fatalf("unexpected tiger auth override: %#v", cfg.Tiger)
	}
}

func TestSchemaPathCandidates(t *testing.T) {
	cfg := DefaultRuntime()
	cfg.Paths.SchemaDir = "config/schema"

	got := cfg.SchemaPathCandidates("crypto_options.sql")
	want := []string{
		filepath.Join("config", "schema", "crypto_options.sql"),
		filepath.Join("..", "config", "schema", "crypto_options.sql"),
		filepath.Join("..", "..", "config", "schema", "crypto_options.sql"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SchemaPathCandidates() = %#v, want %#v", got, want)
	}
}
