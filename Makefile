.PHONY: run test verify

run:
	go run ./cmd/portunus

test:
	go test ./...

verify:
	@test -z "$$(gofmt -l cmd internal)" || { gofmt -l cmd internal; exit 1; }
	go vet ./...
	go test -race ./...
