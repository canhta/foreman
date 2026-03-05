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
