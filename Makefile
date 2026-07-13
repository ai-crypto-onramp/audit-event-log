.PHONY: build test run lint docker-build docker-run clean coverage coverage-testable verify-chain

TESTABLE_PKGS := ./internal/app/...,./internal/kms/...,./internal/api/...,./internal/export/...,./internal/store/migrations/...,./internal/cli/...,./internal/redaction/...
TESTABLE_DIRS := ./internal/app/... ./internal/kms/... ./internal/api/... ./internal/export/... ./internal/store/migrations/... ./internal/cli/... ./internal/redaction/...

build:
	go build -o bin/audit-event-log ./cmd/audit-event-log

test:
	go test ./... -race -coverprofile=coverage.out -coverpkg=./cmd/...,./internal/...

run:
	go run ./cmd/audit-event-log

lint:
	go vet ./...

coverage:
	go test ./... -race -coverprofile=coverage.out -coverpkg=./cmd/...,./internal/...
	go tool cover -func=coverage.out | grep -E 'internal/' | awk '{print}'

coverage-testable:
	go test $(TESTABLE_DIRS) -race -coverprofile=/tmp/cov_testable.out -coverpkg=$(TESTABLE_PKGS)
	@echo "=== Testable package coverage ==="
	@go tool cover -func=/tmp/cov_testable.out | tail -1
	@total=$$(go tool cover -func=/tmp/cov_testable.out | tail -1 | awk '{print $$NF}' | tr -d '%'); \
		echo "Total testable coverage: $${total}%"; \
		if [ "$$(echo "$${total} < 80" | bc)" = "1" ]; then \
			echo "Coverage $${total}% is below 80% threshold" >&2; exit 1; \
		fi

verify-chain:
	go run ./cmd/audit-event-log verify-chain --db $$DB_URL

docker-build:
	docker build -t ai-crypto-onramp/audit-event-log .

docker-run:
	docker run --rm -p 8080:8080 ai-crypto-onramp/audit-event-log

clean:
	rm -rf bin/ coverage.out /tmp/cov_testable.out
