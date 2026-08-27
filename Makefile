# SPDX-FileCopyrightText: 2026 Ryan Madhuwala
# SPDX-License-Identifier: MIT

APP := linkedin-api
BIN := bin/$(APP)

.PHONY: all build ui run test test-race cover bench lint fmt tidy docker-build clean

all: ui build

ui:
	cd web && npm ci && npm run build

build:
	CGO_ENABLED=0 go build -ldflags "-s -w" -o $(BIN) ./cmd/server

run:
	go run ./cmd/server

test:
	go test ./... -count=1

test-race:
	go test ./... -race -count=1

cover:
	go test ./... -coverprofile=coverage.out -covermode=atomic
	go tool cover -func=coverage.out | tail -1

bench:
	go test -bench=. -benchmem ./internal/linkedin/parse

fmt:
	gofmt -w .

lint:
	gofmt -l .
	go vet ./...
	@command -v golangci-lint >/dev/null && golangci-lint run || echo "golangci-lint not installed, skipping"

tidy:
	go mod tidy

docker-build:
	docker build -f deploy/Dockerfile -t $(APP):latest .

clean:
	rm -rf bin coverage.out
