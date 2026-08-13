package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Cyvadra/toktik/internal/config"
	"github.com/Cyvadra/toktik/internal/dto"
	deribitpkg "github.com/Cyvadra/toktik/pkg/deribit"
	"github.com/gin-gonic/gin"
)

type mockDeribitProvider struct {
	response        *dto.DeribitOptionChainResponse
	historyResponse *dto.DeribitOptionChainHistoryResponse
	request         dto.DeribitOptionChainRequest
	err             error
}

func (m *mockDeribitProvider) QueryOptionChain(_ context.Context, request dto.DeribitOptionChainRequest) (*dto.DeribitOptionChainResponse, error) {
	m.request = request
	return m.response, m.err
}

func (m *mockDeribitProvider) QueryOptionChainHistory(_ context.Context, request dto.DeribitOptionChainRequest) (*dto.DeribitOptionChainHistoryResponse, error) {
	m.request = request
	return m.historyResponse, m.err
}

func TestGetDeribitOptionChainSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &mockDeribitProvider{response: &dto.DeribitOptionChainResponse{Data: []dto.DeribitOptionChainContract{
		{Contract: dto.DeribitOptionContract{Ticker: "BTC-28AUG26-110000-P"}, PremiumCurrency: "BTC"},
	}}}
	router := NewRouterFromDeps(Deps{Config: config.DefaultRuntime(), Deribit: provider})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/deribit/options/chain?underlying=btc&date=2026-08-13&contract_type=put&limit=10", nil)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d want 200 body=%s", recorder.Code, recorder.Body.String())
	}
	if provider.request.Underlying != "btc" || provider.request.Date != "2026-08-13" || provider.request.ContractType != "put" || provider.request.Limit != 10 {
		t.Fatalf("unexpected request: %#v", provider.request)
	}
	if !strings.Contains(recorder.Body.String(), "BTC-28AUG26-110000-P") {
		t.Fatalf("unexpected response: %s", recorder.Body.String())
	}
}

func TestGetDeribitOptionChainHistorySuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &mockDeribitProvider{historyResponse: &dto.DeribitOptionChainHistoryResponse{Data: []dto.DeribitOptionChainSnapshot{{Date: "2026-08-12", Data: []dto.DeribitOptionChainContract{{Contract: dto.DeribitOptionContract{Ticker: "BTC-28AUG26-110000-P"}}}}}}}
	router := NewRouterFromDeps(Deps{Config: config.DefaultRuntime(), Deribit: provider})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/deribit/options/chain/history?underlying=BTC&from=2026-08-12&to=2026-08-13&contract_type=put", nil)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d want 200 body=%s", recorder.Code, recorder.Body.String())
	}
	if provider.request.From != "2026-08-12" || provider.request.To != "2026-08-13" || provider.request.ContractType != "put" {
		t.Fatalf("unexpected request: %#v", provider.request)
	}
	if !strings.Contains(recorder.Body.String(), "2026-08-12") || !strings.Contains(recorder.Body.String(), "BTC-28AUG26-110000-P") {
		t.Fatalf("unexpected response: %s", recorder.Body.String())
	}
}

func TestGetDeribitOptionChainNotConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouterFromDeps(Deps{Config: config.DefaultRuntime()})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/deribit/options/chain?underlying=BTC", nil)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotImplemented {
		t.Fatalf("status=%d want 501 body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestGetDeribitOptionChainRequiresUnderlying(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &mockDeribitProvider{}
	router := NewRouterFromDeps(Deps{Config: config.DefaultRuntime(), Deribit: provider})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/deribit/options/chain", nil)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want 400 body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestGetDeribitOptionChainValidationError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &mockDeribitProvider{err: dto.NewValidationError("contract_type must be call or put")}
	router := NewRouterFromDeps(Deps{Config: config.DefaultRuntime(), Deribit: provider})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/deribit/options/chain?underlying=BTC&contract_type=future", nil)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want 400 body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestGetDeribitOptionChainSanitizesUpstreamError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &mockDeribitProvider{err: &deribitpkg.RequestError{Err: errors.New("proxy http://secret-proxy failed")}}
	router := NewRouterFromDeps(Deps{Config: config.DefaultRuntime(), Deribit: provider})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/deribit/options/chain?underlying=BTC", nil)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status=%d want 502 body=%s", recorder.Code, recorder.Body.String())
	}
	var response dto.ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Error != "deribit upstream error" {
		t.Fatalf("error=%q want sanitized upstream error", response.Error)
	}
}

func TestGetDeribitOptionChainSanitizesMalformedResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &mockDeribitProvider{err: &deribitpkg.ResponseError{Message: "invalid instrument"}}
	router := NewRouterFromDeps(Deps{Config: config.DefaultRuntime(), Deribit: provider})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/deribit/options/chain?underlying=BTC", nil)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadGateway || strings.Contains(recorder.Body.String(), "invalid instrument") {
		t.Fatalf("expected sanitized 502, got status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
