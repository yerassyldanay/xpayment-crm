.PHONY: build run test vet fmt tidy docker

build:
	CGO_ENABLED=0 go build -trimpath -o bin/xpayment-crm ./cmd/main.go

run:
	go run ./cmd/main.go

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

tidy:
	go mod tidy

docker:
	docker build -t xpayment-crm:local .
