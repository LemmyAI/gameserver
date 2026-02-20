.PHONY: run build test proto clean test-client test-server

# Default target - build and run server
run: build
	./bin/server

# Build both server and client
build:
	go build -o bin/server ./cmd/server
	go build -o bin/client ./cmd/client

# Build only server
server:
	go build -o bin/server ./cmd/server

# Build only client
client:
	go build -o bin/client ./cmd/client

# Run tests
test:
	go test -v ./...

# Generate protobuf code
proto:
	protoc --go_out=. --go_opt=module=github.com/LemmyAI/gameserver proto/game.proto

# Test server (run in background)
test-server: build
	./bin/server &

# Test client
test-client: build
	./bin/client

# Clean build artifacts
clean:
	rm -rf bin/

# Development loop
dev: build
	./bin/server

# Install protoc dependencies (run once)
install-tools:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest

# Benchmarks
bench:
	go test -bench=. ./...