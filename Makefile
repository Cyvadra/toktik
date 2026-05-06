BUILD_DIR := bin

GOFLAGS := -trimpath
LDFLAGS := -s -w

`.PHONY: build-convert build-import build-missing-days build-kline-backfill build-kline-migrate-utc build-symbol-id-migrate build-volume-migrate build-api build-backtest-example build-backtest-portfolio build-backtest-btc-portfolio build-us-market-import build-feature-store-backfill web-install web-dev web-build swagger-docs export-market-api-md refresh-api-docs build-all build-win-arm clean

build-all: build-convert build-import build-missing-days build-kline-backfill build-kline-migrate-utc build-symbol-id-migrate build-volume-migrate build-api build-backtest-example build-backtest-portfolio build-us-market-import build-feature-store-backfill

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

web-install:
	npm --prefix web/data-browser install

web-dev:
	npm --prefix web/data-browser run dev

web-build:
	npm --prefix web/data-browser run build

swagger-docs:
	go run github.com/swaggo/swag/cmd/swag@v1.8.12 init --parseDependency --parseInternal --exclude tmp -g cmd/api-server/main.go -o docs

export-market-api-md:
	go run ./cmd/api-docs-markdown -input docs/swagger.json -output docs/db-market-indicator-api.md

refresh-api-docs: swagger-docs export-market-api-md

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
