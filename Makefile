BIN_DIR := bin
GO := $(shell if [ -x /opt/homebrew/bin/go ]; then echo /opt/homebrew/bin/go; else echo go; fi)

.PHONY: build run test clean

build:
	mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN_DIR)/ppw ./cmd/ppw

run: build
	$(BIN_DIR)/ppw --help

test:
	$(GO) test ./internal/... -count=1

clean:
	rm -rf $(BIN_DIR)
