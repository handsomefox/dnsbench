BIN := dnsbench
PKG := ./...

# default flags for the benchmark;
#   make run N=10 TIMEOUT=2s
N ?= 10
TIMEOUT ?= 2s
RESFILE ?=               # e.g. -f myresolvers.txt

.PHONY: all build run bench fmt vet tidy test clean

all: build

build:
	go build -ldflags '-w -s' -tags netgo -o $(BIN) .
	GOOS=windows GOARCH=amd64 go build -ldflags '-w -s' -tags netgo -o $(BIN).exe .

run: build
	./$(BIN) -n $(N) -t $(TIMEOUT) $(RESFILE)
