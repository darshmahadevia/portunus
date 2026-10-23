.PHONY: run test web verify verify-go verify-web demo benchmark

run:
	go run ./cmd/portunus

test:
	go test ./...

web:
	npm --prefix web ci
	npm --prefix web run build

verify: verify-web verify-go

verify-web:
	npm --prefix web ci
	npm --prefix web run check
	npm --prefix web test
	npm --prefix web run build

verify-go:
	@test -z "$$(gofmt -l cmd internal)" || { gofmt -l cmd internal; exit 1; }
	go vet ./...
	go test -race ./...

demo:
	go run ./cmd/portunus-bench -verify -output artifacts/verification.json

benchmark:
	go run ./cmd/portunus-bench -rounds 5 -requests 100 -concurrency 8 -output docs/benchmarks/latest.json
	node scripts/summarize-benchmark.mjs
