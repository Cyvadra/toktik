BUILD_DIR := bin

GOFLAGS := -trimpath
LDFLAGS := -s -w

.PHONY: build-convert build-import build-missing-days build-kline-backfill build-kline-migrate-utc build-symbol-id-migrate build-volume-migrate build-api build-api-smoke build-backtest-example build-backtest-portfolio build-backtest-btc-portfolio build-us-market-import build-feature-store-backfill build-frontend frontend-dev web-install web-dev web-build swagger-fmt swagger-market-docs swagger-backtests-docs swagger-third-party-docs export-market-api-md export-backtests-api-md export-third-party-docs refresh-api-docs build-all build-win-arm clean

build-all: build-convert build-import build-missing-days build-kline-backfill build-kline-migrate-utc build-symbol-id-migrate build-volume-migrate build-api build-api-smoke build-backtest-example build-backtest-portfolio build-us-market-import build-feature-store-backfill

build-convert:
	@mkdir -p $(BUILD_DIR)
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BUILD_DIR)/crypto-options-convert ./cmd/crypto-options-convert

build-import:
	@mkdir -p $(BUILD_DIR)
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BUILD_DIR)/crypto-options-import ./cmd/crypto-options-import

build-missing-days:
	@mkdir -p $(BUILD_DIR)
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BUILD_DIR)/market-missing-days ./cmd/market-missing-days

build-kline-backfill:
	@mkdir -p $(BUILD_DIR)
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BUILD_DIR)/options-kline-backfill ./cmd/options-kline-backfill

build-kline-migrate-utc:
	@mkdir -p $(BUILD_DIR)
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BUILD_DIR)/crypto-options-kline-migrate-utc ./cmd/crypto-options-kline-migrate-utc

build-symbol-id-migrate:
	@mkdir -p $(BUILD_DIR)
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BUILD_DIR)/crypto-options-symbol-id-migrate ./cmd/crypto-options-symbol-id-migrate

build-volume-migrate:
	@mkdir -p $(BUILD_DIR)
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BUILD_DIR)/market-volume-migrate ./cmd/market-volume-migrate

build-api:
	@mkdir -p $(BUILD_DIR)
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BUILD_DIR)/api-server ./cmd/api-server

build-api-smoke:
	@mkdir -p $(BUILD_DIR)
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BUILD_DIR)/api-smoke ./cmd/api-smoke

build-backtest-example:
	@mkdir -p $(BUILD_DIR)
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BUILD_DIR)/backtest-example ./cmd/backtest-example

build-backtest-portfolio:
	@mkdir -p $(BUILD_DIR)
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BUILD_DIR)/backtest-portfolio ./cmd/backtest-portfolio

build-backtest-btc-portfolio: build-backtest-portfolio

build-us-market-import:
	@mkdir -p $(BUILD_DIR)
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BUILD_DIR)/us-market-import ./cmd/us-market-import

build-feature-store-backfill:
	@mkdir -p $(BUILD_DIR)
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BUILD_DIR)/feature-store-backfill ./cmd/feature-store-backfill

build-frontend:
	@mkdir -p $(BUILD_DIR)
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BUILD_DIR)/dsl-backtest-frontend ./frontend

frontend-dev:
	go run ./frontend

web-install:
	npm --prefix web/data-browser install

web-dev:
	npm --prefix web/data-browser run dev

web-build:
	npm --prefix web/data-browser run build

swagger-fmt:
	go run github.com/swaggo/swag/cmd/swag@v1.8.12 fmt -g cmd/api-server/main.go

swagger-market-docs:
	go run github.com/swaggo/swag/cmd/swag@v1.8.12 init --parseDependency --parseInternal --exclude tmp -g cmd/api-server/main.go --tags 'Indicators,Factors,Fundamentals,Macro,Features,USStocks,USOptions,Screener,Calendar,Utilities' --outputTypes json,yaml -o docs/swagger/market

swagger-backtests-docs:
	go run github.com/swaggo/swag/cmd/swag@v1.8.12 init --parseDependency --parseInternal --exclude tmp -g cmd/api-server/main.go --tags 'Universes,Backtests,Strategies' --outputTypes json,yaml -o docs/swagger/backtests

swagger-third-party-docs:
	go run github.com/swaggo/swag/cmd/swag@v1.8.12 init --parseDependency --parseInternal --exclude tmp -g cmd/api-server/main.go --tags 'Polygon,Deribit' --outputTypes json,yaml -o docs/swagger/third-party

export-market-api-md: swagger-market-docs
	go run ./cmd/api-docs-markdown -input docs/swagger/market/swagger.json -output docs/db-market-indicator-api.md

export-backtests-api-md: swagger-backtests-docs
	go run ./cmd/api-docs-markdown -scope backtests -input docs/swagger/backtests/swagger.json -output docs/dsl.md -title "Backtests & DSL API"

export-third-party-docs: swagger-third-party-docs
	go run ./cmd/api-docs-markdown -scope third-party -input docs/swagger/third-party/swagger.json -output docs/third-party-api-rt.md -title "Third-Party Realtime Market Data API"

export-vscode-dsl-extension:
	go run ./cmd/vscode-dsl-extension-data -output extension/vscode

refresh-api-docs: swagger-fmt export-market-api-md export-backtests-api-md

build-win-arm:
	@mkdir -p $(BUILD_DIR)
	GOOS=windows GOARCH=arm64 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BUILD_DIR)/crypto-options-convert.exe ./cmd/crypto-options-convert
	GOOS=windows GOARCH=arm64 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BUILD_DIR)/crypto-options-import.exe ./cmd/crypto-options-import
	GOOS=windows GOARCH=arm64 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BUILD_DIR)/market-missing-days.exe ./cmd/market-missing-days
	GOOS=windows GOARCH=arm64 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BUILD_DIR)/options-kline-backfill.exe ./cmd/options-kline-backfill
	GOOS=windows GOARCH=arm64 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BUILD_DIR)/crypto-options-kline-migrate-utc.exe ./cmd/crypto-options-kline-migrate-utc

build-darwin-amd64:
	@mkdir -p $(BUILD_DIR)
	GOOS=darwin GOARCH=amd64 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BUILD_DIR)/crypto-options-convert ./cmd/crypto-options-convert
	GOOS=darwin GOARCH=amd64 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BUILD_DIR)/crypto-options-import ./cmd/crypto-options-import
	GOOS=darwin GOARCH=amd64 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BUILD_DIR)/market-missing-days ./cmd/market-missing-days
	GOOS=darwin GOARCH=amd64 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BUILD_DIR)/options-kline-backfill ./cmd/options-kline-backfill
	GOOS=darwin GOARCH=amd64 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BUILD_DIR)/crypto-options-kline-migrate-utc ./cmd/crypto-options-kline-migrate-utc

clean:
	rm -rf $(BUILD_DIR)
