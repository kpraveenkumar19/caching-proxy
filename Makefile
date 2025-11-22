.PHONY: build run clean

BINARY=caching-proxy

build:
	go build -o bin/$(BINARY) ./cmd/caching-proxy

run:
	go run ./cmd/caching-proxy

clean:
	rm -rf bin


