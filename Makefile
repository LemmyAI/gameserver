.PHONY: run build test proto clean

# Default target
run: build
	./bin/server

# Build the server
build:
	go build -o bin/server ./cmd/server

# Run tests
test:
	go test -v ./...

# Generate protobuf (Phase 1+)
proto:
	protoc --go_out=. --go_opt=paths=source_relative \
		proto/game.proto

# Clean build artifacts
clean:
	rm -rf bin/

# Quick dev loop
dev: build
	./bin/server