VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS = -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

build:
	go build -ldflags "$(LDFLAGS)" -o bin/xget xget/src

lint:
	golangci-lint --timeout=5m run

go-tidy:
	go mod tidy -v

go-update:
	go get -u ./...
