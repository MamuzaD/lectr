.PHONY: build test vet release clean

build:
	go build -o lectr ./cmd/lectr

test:
	go test ./...

vet:
	go vet ./...

release:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o bin/lectr-darwin-arm64 ./cmd/lectr

clean:
	go clean
