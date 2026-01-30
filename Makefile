.PHONY: all lint test fmt govulncheck

all: lint test fmt govulncheck

lint:
	golangci-lint run

govulncheck:
	govulncheck ./...

gosec:
	gosec ./...

get-tools:
	go install github.com/securego/gosec/cmd/gosec@latest
	go install golang.org/x/vuln/cmd/govulncheck@latest
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest

generate:
	cd tools; go generate ./...

fmt:
	gofmt -s -w -e .

test:
	go test -v -cover -timeout=120s -parallel=10 ./...

vulncheck:
	govulncheck -format text ./...

install:
	go install .
