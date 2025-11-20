.DEFAULT_GOAL := all
BIN := ./bin/dnsbench
PKG := ./...
UI_DIR := ./webui

# default flags for the benchmark;
#   make run N=10 TIMEOUT=2s
N ?= 10
TIMEOUT ?= 3s
RESFILE ?=               # e.g. -f myresolvers.txt

.PHONY: all build build-windows test run ui-install ui-build ui-dev run-ui

all: build

ui-install:
	@cd $(UI_DIR) && npm install

ui-build:
	@echo "Building web UI..."
	@mkdir -p $(UI_DIR)/dist
	@cd $(UI_DIR) && [ -d node_modules ] || npm install
	@cd $(UI_DIR) && npm run build

ui-dev:
	@cd $(UI_DIR) && npm run dev -- --host

build: ui-build test
	@echo "Building dnsbench..."
	@go build -ldflags '-w -s' -tags netgo -o $(BIN) .
	@echo "Build complete: $(BIN)"

build-windows: ui-build test
	@echo "Building dnsbench for Windows..."
	@GOOS=windows GOARCH=amd64 go build -ldflags '-w -s' -tags netgo -o $(BIN).exe .
	@echo "Build complete: $(BIN).exe"

test:
	@echo "Running tests..."
	@go test -v ./...
	@echo "Tests completed successfully."

run: build
	@echo "Running dnsbench with N=$(N), TIMEOUT=$(TIMEOUT), RESFILE=$(RESFILE)..."
	./$(BIN) -n $(N) -t $(TIMEOUT) $(RESFILE)
	@echo "Run completed."

run-ui: build
	@echo "Starting dnsbench Web UI on http://localhost:8080 ..."
	./$(BIN) -ui -listen :8080
