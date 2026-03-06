.PHONY: build test lint clean setup-hooks coverage

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

setup-hooks:
	git config core.hooksPath .githooks

.PHONY: coverage
coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out
	@total=$$(go tool cover -func=coverage.out | grep "^total:" | awk '{print $$3}' | tr -d '%'); \
	echo "Total coverage: $$total%"; \
	if [ "$$(echo "$$total < 80" | bc -l)" = "1" ]; then \
		echo "FAIL: coverage $$total% is below 80% threshold"; \
		exit 1; \
	fi
	@echo "PASS: coverage meets 80% threshold"

# Cross-platform builds
PLATFORMS := linux-amd64 linux-arm64 darwin-amd64 darwin-arm64 windows-amd64

.PHONY: release
release: $(PLATFORMS)

linux-amd64:
	@mkdir -p dist
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o dist/foreman-linux-amd64 .

linux-arm64:
	@mkdir -p dist
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o dist/foreman-linux-arm64 .

darwin-amd64:
	@mkdir -p dist
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -o dist/foreman-darwin-amd64 .

darwin-arm64:
	@mkdir -p dist
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -o dist/foreman-darwin-arm64 .

windows-amd64:
	@mkdir -p dist
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o dist/foreman-windows-amd64.exe .

.PHONY: docker
docker:
	docker build -t foreman:latest .
