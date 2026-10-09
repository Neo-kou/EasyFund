BINARY = easyfund

.PHONY: build test run clean

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o dist/$(BINARY) ./cmd/easyfund

test:
	go test ./...

run: build
	./dist/$(BINARY) crawl -config config.yaml

clean:
	rm -rf dist
