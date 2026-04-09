package polygon

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	stockMinuteAggregatesDatasetPath  = "us_stocks_sip/minute_aggs_v1"
	optionMinuteAggregatesDatasetPath = "us_options_opra/minute_aggs_v1"
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

func (c *Client) DownloadStockMinuteAggregates(date time.Time, force bool) (string, error) {
	return c.downloadFlatFile(StockMinuteAggregatesDataset(), date, force)
}

func (c *Client) DownloadOptionMinuteAggregates(date time.Time, force bool) (string, error) {
	return c.downloadFlatFile(OptionMinuteAggregatesDataset(), date, force)
}

func (c *Client) downloadFlatFile(dataset FlatFileDataset, date time.Time, force bool) (string, error) {
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
	if err := c.downloadToFile(context.Background(), requestURL, cachePath); err != nil {
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
	return strings.Trim(strings.TrimSpace(dataset.Path), "/") + "/" + day.Format("2006") + "/" + fileName
}

func (c *Client) downloadToFile(ctx context.Context, requestURL string, cachePath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return fmt.Errorf("build flatfile download request: %w", err)
	}
	if err := c.addHeaders(ctx, req); err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("download flatfile %s: %w", requestURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &HTTPStatusError{
			URL:        requestURL,
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			Body:       strings.TrimSpace(string(body)),
		}
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

	if _, err := io.Copy(tempFile, resp.Body); err != nil {
		return fmt.Errorf("write flatfile cache %s: %w", cachePath, err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close temp flatfile %s: %w", tempPath, err)
	}
	if err := os.Rename(tempPath, cachePath); err != nil {
		return fmt.Errorf("replace flatfile cache %s: %w", cachePath, err)
	}
	return nil
}
