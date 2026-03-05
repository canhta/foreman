.PHONY: build test lint clean

BINARY := foreman

build:
	go build -o $(BINARY) .

test:
	go test ./... -v -race

lint:
	go vet ./...
	golangci-lint run

clean:
	rm -f $(BINARY)

# Cross-platform builds
PLATFORMS := linux-amd64 linux-arm64 darwin-amd64 darwin-arm64 windows-amd64

.PHONY: release
release: $(PLATFORMS)

linux-amd64:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=1 go build -o dist/foreman-linux-amd64 .

linux-arm64:
	GOOS=linux GOARCH=arm64 CGO_ENABLED=1 go build -o dist/foreman-linux-arm64 .

darwin-amd64:
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=1 go build -o dist/foreman-darwin-amd64 .

darwin-arm64:
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 go build -o dist/foreman-darwin-arm64 .

windows-amd64:
	GOOS=windows GOARCH=amd64 CGO_ENABLED=1 go build -o dist/foreman-windows-amd64.exe .

.PHONY: docker
docker:
	docker build -t foreman:latest .
