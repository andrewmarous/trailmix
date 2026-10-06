.PHONY: build test vet clean

build:
	mkdir -p build
	go build -o build/tmx ./cmd/tmx

test:
	go test -race ./...

vet:
	go vet ./...

clean:
	rm -rf build
