.PHONY: build-convert build-import build-missing-days build-api build-backtest-example build-all build-win-arm clean

BUILD_DIR := bin

GOFLAGS := -trimpath
LDFLAGS := -s -w

build-all: build-convert build-import build-missing-days build-api build-backtest-example

build-convert:
	@mkdir -p $(BUILD_DIR)
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BUILD_DIR)/crypto-options-convert ./cmd/crypto-options-convert

build-import:
	@mkdir -p $(BUILD_DIR)
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BUILD_DIR)/crypto-options-import ./cmd/crypto-options-import

build-missing-days:
	@mkdir -p $(BUILD_DIR)
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BUILD_DIR)/crypto-options-missing-days ./cmd/crypto-options-missing-days

build-api:
	@mkdir -p $(BUILD_DIR)
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BUILD_DIR)/api-server ./cmd/api-server

build-backtest-example:
	@mkdir -p $(BUILD_DIR)
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BUILD_DIR)/backtest-example ./cmd/backtest-example

build-win-arm:
	@mkdir -p $(BUILD_DIR)
	GOOS=windows GOARCH=arm64 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BUILD_DIR)/crypto-options-convert.exe ./cmd/crypto-options-convert
	GOOS=windows GOARCH=arm64 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BUILD_DIR)/crypto-options-import.exe ./cmd/crypto-options-import
	GOOS=windows GOARCH=arm64 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BUILD_DIR)/crypto-options-missing-days.exe ./cmd/crypto-options-missing-days

build-darwin-amd64:
	@mkdir -p $(BUILD_DIR)
	GOOS=darwin GOARCH=amd64 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BUILD_DIR)/crypto-options-convert ./cmd/crypto-options-convert
	GOOS=darwin GOARCH=amd64 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BUILD_DIR)/crypto-options-import ./cmd/crypto-options-import
	GOOS=darwin GOARCH=amd64 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BUILD_DIR)/crypto-options-missing-days ./cmd/crypto-options-missing-days

clean:
	rm -rf $(BUILD_DIR)
