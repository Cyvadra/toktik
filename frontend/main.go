package main

import (
	"embed"
	"flag"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:embed templates static
var assets embed.FS

type config struct {
	listenAddr  string
	apiBaseURL  *url.URL
	apiKey      string
	reportDir   string
	strategyDir string
}

func main() {
	cfg, err := parseConfig()
	if err != nil {
		log.Fatal(err)
	}

	store, err := newStrategyStore(cfg.strategyDir)
	if err != nil {
		log.Fatal(err)
	}
	templates, err := template.ParseFS(assets, "templates/*.html")
	if err != nil {
		log.Fatal(err)
	}
	staticFS, err := fs.Sub(assets, "static")
	if err != nil {
		log.Fatal(err)
	}

	app := newApp(store, newAPIClient(cfg.apiBaseURL, cfg.apiKey), templates, cfg.reportDir)
	mux := http.NewServeMux()
	app.routes(mux)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	server := &http.Server{
		Addr:              cfg.listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("DSL backtest frontend listening on http://%s", cfg.listenAddr)
	log.Fatal(server.ListenAndServe())
}

func parseConfig() (config, error) {
	listenAddr := flag.String("listen", envOr("TOKTIK_FRONTEND_LISTEN", "127.0.0.1:9020"), "frontend listen address")
	apiBase := flag.String("api-base-url", envOr("TOKTIK_FRONTEND_API_BASE_URL", "http://127.0.0.1:9010"), "Toktik API base URL")
	apiKey := flag.String("api-key", os.Getenv("TOKTIK_API_KEY"), "Toktik API key (defaults to TOKTIK_API_KEY or root toktik-api-key)")
	reportDir := flag.String("report-dir", envOr("TOKTIK_FRONTEND_REPORT_DIR", "reports/backtests"), "directory containing generated backtest HTML reports")
	strategyDir := flag.String("strategy-dir", envOr("TOKTIK_FRONTEND_STRATEGY_DIR", "pkg/dsl/scripts/strategies"), "directory containing .toktik strategies")
	flag.Parse()

	parsedURL, err := url.Parse(strings.TrimRight(strings.TrimSpace(*apiBase), "/"))
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return config{}, fmt.Errorf("invalid API base URL %q", *apiBase)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return config{}, fmt.Errorf("API base URL must use http or https")
	}
	resolvedStrategyDir, err := resolveStrategyDir(strings.TrimSpace(*strategyDir))
	if err != nil {
		return config{}, err
	}
	resolvedReportDir, err := resolveReportDir(strings.TrimSpace(*reportDir))
	if err != nil {
		return config{}, err
	}
	resolvedAPIKey, err := resolveAPIKey(strings.TrimSpace(*apiKey), resolvedStrategyDir)
	if err != nil {
		return config{}, err
	}
	return config{
		listenAddr:  strings.TrimSpace(*listenAddr),
		apiBaseURL:  parsedURL,
		apiKey:      resolvedAPIKey,
		reportDir:   resolvedReportDir,
		strategyDir: resolvedStrategyDir,
	}, nil
}

func resolveAPIKey(explicitKey, strategyDir string) (string, error) {
	if explicitKey != "" {
		return explicitKey, nil
	}
	candidates := []string{"toktik-api-key"}
	if strategyDir != "" {
		candidates = append(candidates, filepath.Join(strategyDir, "..", "..", "..", "..", "toktik-api-key"))
	}
	for _, candidate := range candidates {
		contents, err := os.ReadFile(filepath.Clean(candidate))
		if err == nil {
			key := strings.TrimSpace(string(contents))
			if key == "" {
				return "", fmt.Errorf("API key file %s is empty", candidate)
			}
			return key, nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("read API key file %s: %w", candidate, err)
		}
	}
	return "", nil
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func resolveStrategyDir(dir string) (string, error) {
	if filepath.IsAbs(dir) {
		return dir, nil
	}
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir, nil
	}
	executable, err := os.Executable()
	if err == nil {
		candidate := filepath.Join(filepath.Dir(executable), "..", dir)
		if info, statErr := os.Stat(candidate); statErr == nil && info.IsDir() {
			return filepath.Clean(candidate), nil
		}
	}
	return "", fmt.Errorf("strategy directory %q was not found from the working directory or executable location", dir)
}

func resolveReportDir(dir string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", fmt.Errorf("report directory is required")
	}
	resolved, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve report directory: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("report directory %q: %w", dir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("report directory %q is not a directory", dir)
	}
	return filepath.Clean(resolved), nil
}
