.PHONY: all lint test fmt

all: lint test fmt

lint:
	golangci-lint run

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
