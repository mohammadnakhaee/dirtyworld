.PHONY: server down build test fmt vet

# Run the full stack (Go server + nginx) → http://localhost
server:
	docker compose up --build

# Stop the stack and remove its containers.
down:
	docker compose down

# Compile a binary to bin/server.
build:
	go build -o bin/server ./cmd/server

# Run the test suite.
test:
	go test ./...

# Format all Go sources.
fmt:
	gofmt -w internal cmd

# Static checks.
vet:
	go vet ./...
