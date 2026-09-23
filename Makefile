.DEFAULT_GOAL := all
BIN := ./bin/dnsbench

# default flags for the benchmark;
#   make run N=10 TIMEOUT=2s
N ?= 10
TIMEOUT ?= 3s
RESFILE ?=               # e.g. -f myresolvers.txt

.PHONY: all build build-windows test lint run run-ui

all: build

build: test
	@echo "Building dnsbench..."
	@go build -ldflags '-w -s' -tags netgo -o $(BIN) .
	@echo "Build complete: $(BIN)"

build-windows: test
	@echo "Building dnsbench for Windows..."
	@GOOS=windows GOARCH=amd64 go build -ldflags '-w -s' -tags netgo -o $(BIN).exe .
	@echo "Build complete: $(BIN).exe"

test:
	@echo "Running tests..."
	@go test -race ./...
	@echo "Tests completed successfully."

lint:
	@gofmt -l . | (! grep .) || (echo "gofmt needs to run on the files above"; exit 1)
	@go vet ./...
	@golangci-lint run ./...

run: build
	@echo "Running dnsbench with N=$(N), TIMEOUT=$(TIMEOUT), RESFILE=$(RESFILE)..."
	./$(BIN) -n $(N) -t $(TIMEOUT) $(RESFILE)
	@echo "Run completed."

run-ui: build
	@echo "Starting dnsbench Web UI on http://127.0.0.1:8080 ..."
	./$(BIN) -ui -listen 127.0.0.1:8080
