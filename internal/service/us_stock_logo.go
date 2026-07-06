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
	if logo, ok, err := s.repo.Find(ctx, normalized); err != nil {
		return nil, fmt.Errorf("query US stock logo: %w", err)
	} else if ok {
		return logoRecordToResponse(*logo, false)
	}
	if s.recentMiss(ctx, normalized) {
		return defaultUSStockLogo(normalized), nil
	}
	logo, err := s.fetchAndStore(ctx, normalized)
	if err == nil {
		return logo, nil
	}
	if errors.Is(err, errUSStockLogoNotFound) {
		_ = s.rememberMiss(ctx, normalized)
		return defaultUSStockLogo(normalized), nil
	}
	return nil, err
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
