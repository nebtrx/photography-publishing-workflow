BIN_DIR := bin

.PHONY: build run test clean

build:
	mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/ppw ./cmd/ppw

run: build
	$(BIN_DIR)/ppw --help

test:
	go test ./internal/... -count=1

clean:
	rm -rf $(BIN_DIR)
