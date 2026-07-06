package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/internal/cache"
	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/Cyvadra/toktik/internal/logorepo"
	"github.com/Cyvadra/toktik/pkg/fmp"
)

const (
	usStockLogoMissTTL     = 24 * time.Hour
	usStockLogoMaxBytes    = 1 << 20
	usStockLogoHTTPTimeout = 20 * time.Second
)

var errUSStockLogoNotFound = errors.New("us stock logo not found")

type USStockLogoService struct {
	repo       *logorepo.Repo
	fmpClient  fmpCompanyProfiler
	cache      cache.Store
	httpClient *http.Client
}

func NewUSStockLogoService(repo *logorepo.Repo, fmpClient *fmp.Client, cacheStore cache.Store) *USStockLogoService {
	if repo == nil {
		return nil
	}
	return &USStockLogoService{
		repo:      repo,
		fmpClient: fmpClient,
		cache:     cacheStore,
		httpClient: &http.Client{
			Timeout: usStockLogoHTTPTimeout,
		},
	}
}

func (s *USStockLogoService) GetLogo(ctx context.Context, symbol string) (*dto.USStockLogoImage, error) {
	normalized := normalizeUSStockCompanyProfileSymbol(symbol)
	if normalized == "" {
		return nil, fmt.Errorf("symbol is required")
	}
	candidates := usStockLogoSymbolCandidates(normalized)
	for _, candidate := range candidates {
		if logo, ok, err := s.repo.Find(ctx, candidate); err != nil {
			return nil, fmt.Errorf("query US stock logo: %w", err)
		} else if ok {
			resp, err := logoRecordToResponse(*logo, false)
			if err != nil {
				return nil, err
			}
			if candidate != normalized {
				if err := s.storeLogoAlias(ctx, normalized, *logo); err != nil {
					slog.Warn("store US stock logo alias failed", "symbol", normalized, "source_symbol", candidate, "err", err)
				}
				resp.Symbol = normalized
			}
			return resp, nil
		}
	}
	if s.allCandidatesRecentlyMissed(ctx, candidates) {
		return defaultUSStockLogo(normalized), nil
	}
	for _, candidate := range candidates {
		if s.recentMiss(ctx, candidate) {
			continue
		}
		logo, err := s.fetchAndStore(ctx, candidate)
		if err == nil {
			if candidate != normalized {
				if err := s.storeLogoAliasFromImage(ctx, normalized, candidate, logo); err != nil {
					slog.Warn("store US stock logo alias failed", "symbol", normalized, "source_symbol", candidate, "err", err)
				}
				logo.Symbol = normalized
			}
			return logo, nil
		}
		if errors.Is(err, errUSStockLogoNotFound) {
			_ = s.rememberMiss(ctx, candidate)
			continue
		}
		return nil, err
	}
	return defaultUSStockLogo(normalized), nil
}

func (s *USStockLogoService) fetchAndStore(ctx context.Context, symbol string) (*dto.USStockLogoImage, error) {
	if s.fmpClient == nil {
		return nil, errUSStockLogoNotFound
	}
	profile, err := s.fmpClient.Profile(ctx, symbol)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no result") {
			return nil, errUSStockLogoNotFound
		}
		return nil, fmt.Errorf("fetch FMP profile: %w", err)
	}
	imageURL := strings.TrimSpace(profile.Image)
	if imageURL == "" || profile.DefaultImage {
		return nil, errUSStockLogoNotFound
	}
	contentType, data, err := s.downloadLogo(ctx, imageURL)
	if err != nil {
		return nil, err
	}
	record := logorepo.StockLogo{
		Symbol:      symbol,
		ContentType: contentType,
		DataBase64:  base64.StdEncoding.EncodeToString(data),
		SourceURL:   imageURL,
		Source:      "fmp",
	}
	if err := s.repo.Upsert(ctx, record); err != nil {
		return nil, fmt.Errorf("store US stock logo: %w", err)
	}
	return &dto.USStockLogoImage{Symbol: symbol, ContentType: contentType, Data: data}, nil
}

func (s *USStockLogoService) downloadLogo(ctx context.Context, imageURL string) (string, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return "", nil, fmt.Errorf("build logo request: %w", err)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("download logo: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", nil, errUSStockLogoNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", nil, fmt.Errorf("download logo: %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, usStockLogoMaxBytes+1))
	if err != nil {
		return "", nil, fmt.Errorf("read logo: %w", err)
	}
	if len(data) == 0 {
		return "", nil, errUSStockLogoNotFound
	}
	if len(data) > usStockLogoMaxBytes {
		return "", nil, fmt.Errorf("logo exceeds %d bytes", usStockLogoMaxBytes)
	}
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if contentType == "" || strings.EqualFold(contentType, "application/octet-stream") {
		contentType = http.DetectContentType(data)
	}
	if !strings.HasPrefix(strings.ToLower(contentType), "image/") {
		return "", nil, fmt.Errorf("logo response is not an image: %s", contentType)
	}
	return contentType, data, nil
}

func (s *USStockLogoService) recentMiss(ctx context.Context, symbol string) bool {
	if s == nil || s.cache == nil {
		return false
	}
	_, ok, err := s.cache.Get(ctx, usStockLogoMissCacheKey(symbol))
	return err == nil && ok
}

func (s *USStockLogoService) rememberMiss(ctx context.Context, symbol string) error {
	if s == nil || s.cache == nil {
		return nil
	}
	return s.cache.Set(ctx, usStockLogoMissCacheKey(symbol), []byte("1"), usStockLogoMissTTL)
}

func usStockLogoMissCacheKey(symbol string) string {
	return "us-stocks:logo:miss:v1:" + normalizeUSStockCompanyProfileSymbol(symbol)
}

func (s *USStockLogoService) allCandidatesRecentlyMissed(ctx context.Context, candidates []string) bool {
	if len(candidates) == 0 {
		return false
	}
	for _, candidate := range candidates {
		if !s.recentMiss(ctx, candidate) {
			return false
		}
	}
	return true
}

func (s *USStockLogoService) storeLogoAlias(ctx context.Context, alias string, source logorepo.StockLogo) error {
	source.Symbol = normalizeUSStockCompanyProfileSymbol(alias)
	if source.Symbol == "" {
		return nil
	}
	return s.repo.Upsert(ctx, source)
}

func (s *USStockLogoService) storeLogoAliasFromImage(ctx context.Context, alias, sourceSymbol string, logo *dto.USStockLogoImage) error {
	if logo == nil {
		return nil
	}
	alias = normalizeUSStockCompanyProfileSymbol(alias)
	if alias == "" {
		return nil
	}
	return s.repo.Upsert(ctx, logorepo.StockLogo{
		Symbol:      alias,
		ContentType: logo.ContentType,
		DataBase64:  base64.StdEncoding.EncodeToString(logo.Data),
		SourceURL:   "alias:" + normalizeUSStockCompanyProfileSymbol(sourceSymbol),
		Source:      "fmp",
	})
}

func usStockLogoSymbolCandidates(symbol string) []string {
	normalized := normalizeUSStockCompanyProfileSymbol(symbol)
	if normalized == "" {
		return nil
	}
	groups := [][]string{
		{"SPY", "SPX"},
		{"QQQ", "NDX"},
		{"GOOGL", "GOOG"},
		{"BRK.B", "BRK.A", "BRK-B", "BRK-A"},
		{"BF.B", "BF.A", "BF-B", "BF-A"},
		{"FOX", "FOXA"},
		{"NWS", "NWSA"},
	}
	for _, group := range groups {
		for _, item := range group {
			if normalized == item {
				return orderedLogoCandidates(normalized, group)
			}
		}
	}
	return []string{normalized}
}

func orderedLogoCandidates(symbol string, group []string) []string {
	out := []string{symbol}
	seen := map[string]struct{}{symbol: {}}
	for _, item := range group {
		item = normalizeUSStockCompanyProfileSymbol(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func logoRecordToResponse(record logorepo.StockLogo, isDefault bool) (*dto.USStockLogoImage, error) {
	data, err := base64.StdEncoding.DecodeString(record.DataBase64)
	if err != nil {
		return nil, fmt.Errorf("decode stored logo: %w", err)
	}
	return &dto.USStockLogoImage{Symbol: record.Symbol, ContentType: record.ContentType, Data: data, Default: isDefault}, nil
}

func defaultUSStockLogo(symbol string) *dto.USStockLogoImage {
	return &dto.USStockLogoImage{Symbol: symbol, ContentType: "image/png", Data: defaultUSStockLogoPNG(), Default: true}
}

func defaultUSStockLogoPNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 96, 96))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.RGBA{R: 238, G: 241, B: 245, A: 255}}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(0, 64, 96, 96), &image.Uniform{C: color.RGBA{R: 45, G: 55, B: 72, A: 255}}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(16, 18, 80, 30), &image.Uniform{C: color.RGBA{R: 45, G: 55, B: 72, A: 255}}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(20, 38, 76, 50), &image.Uniform{C: color.RGBA{R: 88, G: 101, B: 242, A: 255}}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(28, 56, 68, 64), &image.Uniform{C: color.RGBA{R: 45, G: 55, B: 72, A: 255}}, image.Point{}, draw.Src)
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}
