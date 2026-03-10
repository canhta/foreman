.PHONY: build test lint clean reset setup-hooks setup-dev setup-config coverage dev start debug dashboard-build dashboard-dev dashboard-lint dashboard-test ci release docker

BINARY := foreman
GOBIN  := $(shell go env GOPATH)/bin

build: dashboard-build
	go build -o $(BINARY) .

ci: build test lint dashboard-test

# Install development tools (air + dlv). Run once after cloning.
setup-dev:
	go install github.com/air-verse/air@latest
	go install github.com/go-delve/delve/cmd/dlv@latest
	@echo "Dev tools installed to $(GOBIN)"

# Copy foreman.system.toml → ~/.foreman/config.toml for local development.
# Only copies if foreman.system.toml exists and is newer than the destination.
setup-config:
	@if [ -f foreman.system.toml ]; then \
		mkdir -p ~/.foreman; \
		cp -u foreman.system.toml ~/.foreman/config.toml; \
		echo "Synced foreman.system.toml → ~/.foreman/config.toml"; \
	else \
		echo "foreman.system.toml not found — skipping config sync"; \
	fi

# Hot-reload: rebuilds and restarts on file changes (requires air).
# Run 'make setup-dev' once to install. Pass CMD to change sub-command:
#   make dev CMD="run LOCAL-1"
PORT ?= 8080
CMD ?= start --dashboard-port $(PORT)
dev: setup-config
	FOREMAN_DASHBOARD_PORT=$(PORT) $(GOBIN)/air -- $(CMD)

# Build and start the daemon (non-hot-reload).
start: build setup-config
	FOREMAN_DASHBOARD_PORT=$(PORT) ./$(BINARY) $(CMD)

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
	cd internal/dashboard/web && FOREMAN_DASHBOARD_PORT=$(PORT) npm run dev
dashboard-lint:
	cd internal/dashboard/web && npm ci && npm run lint
dashboard-test:
	cd internal/dashboard/web && npm ci && npm run test

clean:
	rm -f $(BINARY)

# Reset all runtime data (projects, legacy DBs, work dirs, WhatsApp session).
# Safe to run repeatedly. Keeps SSH keys in ~/.foreman/ssh and global config untouched.
reset:
	@echo "Resetting Foreman runtime data..."
	rm -rf ~/.foreman/projects/
	rm -f ~/.foreman/projects.json
	rm -f ~/.foreman/*.db ~/.foreman/*.db-wal ~/.foreman/*.db-shm
	rm -rf ~/.foreman/work
	rm -rf ./tmp/foreman
	@echo "Reset complete."

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
release: dashboard-build $(PLATFORMS)

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
