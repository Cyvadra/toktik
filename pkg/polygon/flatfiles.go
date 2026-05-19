package polygon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const (
	stockMinuteAggregatesDatasetPath  = "us_stocks_sip/minute_aggs_v1"
	optionMinuteAggregatesDatasetPath = "us_options_opra/minute_aggs_v1"
	flatFileDownloadMaxAttempts       = 5
	flatFileRetryBaseDelay            = 1500 * time.Millisecond
	flatFileRetryMaxDelay             = 5 * time.Second
)

type FlatFileDataset struct {
	AssetClass string
	DataType   string
	Path       string
}

func StockMinuteAggregatesDataset() FlatFileDataset {
	return FlatFileDataset{
		AssetClass: "stocks",
		DataType:   "minute_aggregates",
		Path:       stockMinuteAggregatesDatasetPath,
	}
}

func OptionMinuteAggregatesDataset() FlatFileDataset {
	return FlatFileDataset{
		AssetClass: "options",
		DataType:   "minute_aggregates",
		Path:       optionMinuteAggregatesDatasetPath,
	}
}

func (c *Client) DownloadStockMinuteAggregates(ctx context.Context, date time.Time, force bool) (string, error) {
	return c.downloadFlatFile(ctx, StockMinuteAggregatesDataset(), date, force)
}

func (c *Client) DownloadOptionMinuteAggregates(ctx context.Context, date time.Time, force bool) (string, error) {
	return c.downloadFlatFile(ctx, OptionMinuteAggregatesDataset(), date, force)
}

func (c *Client) downloadFlatFile(ctx context.Context, dataset FlatFileDataset, date time.Time, force bool) (string, error) {
	if c == nil {
		return "", fmt.Errorf("polygon client is nil")
	}
	if err := c.config.validateFlatFilesConfig(); err != nil {
		return "", err
	}
	marketDate := normalizeFlatFileDate(date)
	if marketDate.IsZero() {
		return "", fmt.Errorf("flatfile date is required")
	}
	if strings.TrimSpace(dataset.Path) == "" {
		return "", fmt.Errorf("flatfile dataset path is required")
	}

	relativePath := flatFileRelativePath(dataset, marketDate)
	cachePath := filepath.Join(c.config.normalizedFlatFilesCacheDir(), filepath.FromSlash(relativePath))
	if !force {
		if info, err := os.Stat(cachePath); err == nil && info.Size() > 0 {
			return cachePath, nil
		} else if err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("stat flatfile cache %s: %w", cachePath, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return "", fmt.Errorf("create flatfile cache directory %s: %w", filepath.Dir(cachePath), err)
	}

	requestURL := c.config.normalizedFlatFilesBaseURL() + "/" + filepath.ToSlash(relativePath)
	if err := c.downloadToFile(ctx, requestURL, relativePath, cachePath); err != nil {
		return "", err
	}
	return cachePath, nil
}

func normalizeFlatFileDate(date time.Time) time.Time {
	if date.IsZero() {
		return time.Time{}
	}
	utc := date.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}

func flatFileRelativePath(dataset FlatFileDataset, date time.Time) string {
	day := normalizeFlatFileDate(date)
	fileName := day.Format("2006-01-02") + ".csv.gz"
	return strings.Trim(strings.TrimSpace(dataset.Path), "/") + "/" + day.Format("2006") + "/" + day.Format("01") + "/" + fileName
}

func (c *Client) downloadToFile(ctx context.Context, requestURL, relativePath, cachePath string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	remote, err := c.config.flatFileRemote(relativePath)
	if err != nil {
		return err
	}

	tempFile, err := os.CreateTemp(filepath.Dir(cachePath), ".polygon-flatfile-*")
	if err != nil {
		return fmt.Errorf("create temp flatfile %s: %w", cachePath, err)
	}
	tempPath := tempFile.Name()
	defer func() {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
	}()

	var stderr bytes.Buffer
	var tool string
	for attempt := 1; attempt <= flatFileDownloadMaxAttempts; attempt++ {
		if err := resetFlatFileTemp(tempFile); err != nil {
			return fmt.Errorf("reset temp flatfile %s: %w", tempPath, err)
		}
		stderr.Reset()

		cmd, cmdTool, err := c.buildFlatFileDownloadCommand(ctx, remote)
		if err != nil {
			return err
		}
		tool = cmdTool
		cmd.Stdout = tempFile
		cmd.Stderr = &stderr
		err = cmd.Run()
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		body := strings.TrimSpace(stderr.String())
		if isMissingFlatFileError(body) {
			return &HTTPStatusError{
				URL:        requestURL,
				StatusCode: http.StatusNotFound,
				Status:     "404 Not Found",
				Body:       body,
			}
		}
		if attempt < flatFileDownloadMaxAttempts && shouldRetryFlatFileDownload(err, body) {
			if waitErr := waitFlatFileRetry(ctx, flatFileRetryDelay(attempt)); waitErr != nil {
				return waitErr
			}
			continue
		}
		if body == "" {
			return fmt.Errorf("download flatfile %s with %s: %w", requestURL, tool, err)
		}
		return fmt.Errorf("download flatfile %s with %s: %w: %s", requestURL, tool, err, body)
	}

	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close temp flatfile %s: %w", tempPath, err)
	}

	info, err := os.Stat(tempPath)
	if err != nil {
		return fmt.Errorf("stat temp flatfile %s: %w", tempPath, err)
	}
	if info.Size() == 0 {
		if tool == "rclone" {
			exists, err := c.rcloneObjectExists(ctx, remote)
			if err != nil {
				return fmt.Errorf("verify flatfile %s with rclone: %w", requestURL, err)
			}
			if !exists {
				return &HTTPStatusError{
					URL:        requestURL,
					StatusCode: http.StatusNotFound,
					Status:     "404 Not Found",
					Body:       "object not found",
				}
			}
		}
		return fmt.Errorf("download flatfile %s with %s: empty file returned", requestURL, tool)
	}
	if err := os.Rename(tempPath, cachePath); err != nil {
		return fmt.Errorf("replace flatfile cache %s: %w", cachePath, err)
	}
	return nil
}

func (c *Client) buildFlatFileDownloadCommand(ctx context.Context, remote flatFileRemote) (*exec.Cmd, string, error) {
	tool := c.config.normalizedFlatFilesTool()
	switch tool {
	case "mc":
		cmd := exec.CommandContext(ctx, "mc", "cat", remote.mcSource)
		cmd.Env = append(os.Environ(), remote.mcAliasEnv)
		return cmd, tool, nil
	case "rclone":
		cmd := exec.CommandContext(ctx, "rclone", "cat", remote.rcloneSource)
		cmd.Env = append(os.Environ(), remote.rcloneEnv...)
		return cmd, tool, nil
	default:
		return nil, "", fmt.Errorf("unsupported polygon flatfile download tool %q", tool)
	}
}

func (c *Client) rcloneObjectExists(ctx context.Context, remote flatFileRemote) (bool, error) {
	cmd := exec.CommandContext(ctx, "rclone", "lsjson", remote.rcloneSource)
	cmd.Env = append(os.Environ(), remote.rcloneEnv...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		body := strings.TrimSpace(stderr.String())
		if body == "" {
			body = strings.TrimSpace(stdout.String())
		}
		if body == "" {
			return false, err
		}
		return false, fmt.Errorf("%w: %s", err, body)
	}

	var entries []struct {
		Name string `json:"Name"`
		Size int64  `json:"Size"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &entries); err != nil {
		return false, fmt.Errorf("parse rclone lsjson output: %w", err)
	}
	return len(entries) > 0, nil
}

type flatFileRemote struct {
	mcSource     string
	mcAliasEnv   string
	rcloneSource string
	rcloneEnv    []string
}

func (c Config) flatFileRemote(relativePath string) (flatFileRemote, error) {
	baseURL := c.normalizedFlatFilesBaseURL()
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return flatFileRemote{}, fmt.Errorf("parse polygon flatfile base url %q: %w", baseURL, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return flatFileRemote{}, fmt.Errorf("invalid polygon flatfile base url %q", baseURL)
	}

	basePath := strings.Trim(strings.TrimSpace(parsed.Path), "/")
	if basePath == "" {
		return flatFileRemote{}, fmt.Errorf("polygon flatfile base url %q must include the bucket path", baseURL)
	}

	endpointURL := url.URL{Scheme: parsed.Scheme, Host: parsed.Host}
	aliasURL := url.URL{
		Scheme: parsed.Scheme,
		Host:   parsed.Host,
		User:   url.UserPassword(c.FlatFilesAccessKey, c.FlatFilesSecretKey),
	}
	objectPath := path.Join(basePath, filepath.ToSlash(relativePath))
	return flatFileRemote{
		mcSource:     mcAliasName + "/" + objectPath,
		mcAliasEnv:   mcAliasEnvName + "=" + aliasURL.String(),
		rcloneSource: rcloneRemoteName + ":" + objectPath,
		rcloneEnv: []string{
			rcloneConfigEnvPrefix + "TYPE=s3",
			rcloneConfigEnvPrefix + "PROVIDER=Other",
			rcloneConfigEnvPrefix + "ACCESS_KEY_ID=" + c.FlatFilesAccessKey,
			rcloneConfigEnvPrefix + "SECRET_ACCESS_KEY=" + c.FlatFilesSecretKey,
			rcloneConfigEnvPrefix + "ENDPOINT=" + endpointURL.String(),
		},
	}, nil
}

const (
	mcAliasName    = "s3massive"
	mcAliasEnvName = "MC_HOST_" + mcAliasName

	rcloneRemoteName      = mcAliasName
	rcloneConfigEnvPrefix = "RCLONE_CONFIG_S3MASSIVE_"
)

func isMissingFlatFileError(output string) bool {
	text := strings.ToLower(strings.TrimSpace(output))
	if text == "" {
		return false
	}
	markers := []string{
		"not found",
		"404",
		"nosuchkey",
		"does not exist",
		"unable to stat source",
		"the specified key does not exist",
	}
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func shouldRetryFlatFileDownload(err error, output string) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(output))
	if text == "" {
		text = strings.ToLower(err.Error())
	} else {
		text += " " + strings.ToLower(err.Error())
	}
	retryMarkers := []string{
		"connection closed by foreign host",
		"connection reset",
		"connection refused",
		"i/o timeout",
		"timed out",
		"timeout",
		"tls handshake timeout",
		"temporary failure",
		"temporarily unavailable",
		"service unavailable",
		"internal server error",
		"bad gateway",
		"gateway timeout",
		"unexpected eof",
		"retry again",
		"no such host",
		"503",
		"502",
		"500",
		"504",
	}
	for _, marker := range retryMarkers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func resetFlatFileTemp(file *os.File) error {
	if err := file.Truncate(0); err != nil {
		return err
	}
	_, err := file.Seek(0, 0)
	return err
}

func flatFileRetryDelay(attempt int) time.Duration {
	delay := flatFileRetryBaseDelay
	for i := 1; i < attempt; i++ {
		if delay >= flatFileRetryMaxDelay {
			return flatFileRetryMaxDelay
		}
		delay *= 2
	}
	if delay > flatFileRetryMaxDelay {
		return flatFileRetryMaxDelay
	}
	return delay
}

func waitFlatFileRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
