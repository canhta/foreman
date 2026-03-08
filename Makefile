.PHONY: build test lint clean setup-hooks setup-dev coverage dev debug dashboard-build dashboard-dev

BINARY := foreman
GOBIN  := $(shell go env GOPATH)/bin

build: dashboard-build
	go build -o $(BINARY) .

# Install development tools (air + dlv). Run once after cloning.
setup-dev:
	go install github.com/air-verse/air@latest
	go install github.com/go-delve/delve/cmd/dlv@latest
	@echo "Dev tools installed to $(GOBIN)"

# Hot-reload: rebuilds and restarts on file changes (requires air).
# Run 'make setup-dev' once to install. Pass CMD to change sub-command:
#   make dev CMD="run LOCAL-1"
CMD ?= start
dev:
	$(GOBIN)/air -- $(CMD)

# Debug build + launch under Delve (requires dlv).
# Run 'make setup-dev' once to install.
# Connect your IDE or use: dlv connect 127.0.0.1:2345
debug:
	CGO_ENABLED=1 go build -gcflags="all=-N -l" -o $(BINARY) .
	$(GOBIN)/dlv exec ./$(BINARY) --headless --listen=:2345 --api-version=2 -- run

test:
	go test ./... -v -race

lint:
	go vet ./...
	golangci-lint run

dashboard-build:
	cd internal/dashboard/web && npm ci && npm run build
dashboard-dev:
	cd internal/dashboard/web && npm run dev

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
