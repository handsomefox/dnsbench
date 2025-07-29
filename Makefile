.DEFAULT_GOAL := all
BIN := ./bin/dnsbench
PKG := ./...

# default flags for the benchmark;
#   make run N=10 TIMEOUT=2s
N ?= 10
TIMEOUT ?= 3s
RESFILE ?=               # e.g. -f myresolvers.txt

.PHONY: all build build-windows test run

all: build build-windows

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
	@go test -v ./...
	@echo "Tests completed successfully."

run: build
	@echo "Running dnsbench with N=$(N), TIMEOUT=$(TIMEOUT), RESFILE=$(RESFILE)..."
	./$(BIN) -n $(N) -t $(TIMEOUT) $(RESFILE)
	@echo "Run completed."
