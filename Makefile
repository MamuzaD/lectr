build:
	go build -o lectr ./cmd/lectr

test:
	go test ./...

vet:
	go vet ./...

clean:
	go clean
