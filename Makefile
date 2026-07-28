.PHONY: build install tidy test deps

# Install Go via Homebrew if missing
deps:
	@which go || brew install go
	go mod tidy

build: deps
	go build -o bin/factory ./cmd/factory

install: build
	cp bin/factory /usr/local/bin/factory

tidy:
	go mod tidy

test:
	go test ./...
