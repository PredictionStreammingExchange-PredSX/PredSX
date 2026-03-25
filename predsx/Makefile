.PHONY: run build clean test

SERVICES := market-discovery token-extractor stream trade-engine orderbook-engine price-engine normalizer analyzer backfill api

run:
	docker-compose -f deployments/docker-compose.yml up --build

build: build-services build-cli

build-services:
	go build ./services/...

build-cli:
	go build -o bin/predsx ./cmd/predsx/main.go

clean:
	docker-compose -f deployments/docker-compose.yml down -v
	find . -name "go.sum" -delete

test:
	go test ./...
