build:
	go build -o bin/xget xget/src

lint:
	golangci-lint --timeout=5m run

go-tidy:
	go mod tidy -v

go-update:
	go get -u ./...
