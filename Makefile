BINARY_NAME=cloudronix-agent
VERSION=0.1.0
BUILD_DIR=build

.PHONY: all build clean test windows linux darwin android

all: build

build:
	go build -ldflags "-X main.version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/cloudronix-agent

# Windows builds
windows: windows-amd64

windows-amd64:
	GOOS=windows GOARCH=amd64 go build -ldflags "-X main.version=$(VERSION)" \
		-o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe ./cmd/cloudronix-agent

# Linux builds
linux: linux-amd64 linux-arm64

linux-amd64:
	GOOS=linux GOARCH=amd64 go build -ldflags "-X main.version=$(VERSION)" \
		-o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/cloudronix-agent

linux-arm64:
	GOOS=linux GOARCH=arm64 go build -ldflags "-X main.version=$(VERSION)" \
		-o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./cmd/cloudronix-agent

# macOS builds
darwin: darwin-amd64 darwin-arm64

darwin-amd64:
	GOOS=darwin GOARCH=amd64 go build -ldflags "-X main.version=$(VERSION)" \
		-o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/cloudronix-agent

darwin-arm64:
	GOOS=darwin GOARCH=arm64 go build -ldflags "-X main.version=$(VERSION)" \
		-o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/cloudronix-agent

# Android build (CLI only, for Termux)
android: android-arm64

android-arm64:
	GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "-X main.version=$(VERSION)" \
		-o $(BUILD_DIR)/$(BINARY_NAME)-android-arm64 ./cmd/cloudronix-agent

# Build all platforms
release: windows linux darwin android
	@echo "Built binaries for all platforms in $(BUILD_DIR)/"
	@ls -la $(BUILD_DIR)/

test:
	go test -v ./...

clean:
	rm -rf $(BUILD_DIR)
	go clean

# Development helpers
dev:
	go run ./cmd/cloudronix-agent $(ARGS)

fmt:
	go fmt ./...

lint:
	golangci-lint run

deps:
	go mod download
	go mod tidy
