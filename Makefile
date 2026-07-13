.PHONY: build test run lint docker-build docker-run clean coverage verify-chain

build:
	go build -o bin/audit-event-log ./cmd/audit-event-log

test:
	go test ./... -race -coverprofile=coverage.out -coverpkg=./...

run:
	go run ./cmd/audit-event-log

lint:
	go vet ./...

coverage:
	go test ./... -race -coverprofile=coverage.out -coverpkg=./...
	go tool cover -func=coverage.out | grep -E 'internal/' | awk '{print}'

verify-chain:
	go run ./cmd/audit-event-log verify-chain --db $$DB_URL

docker-build:
	docker build -t ai-crypto-onramp/audit-event-log .

docker-run:
	docker run --rm -p 8080:8080 ai-crypto-onramp/audit-event-log

clean:
	rm -rf bin/ coverage.out
