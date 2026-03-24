.PHONY: build test race vet lint clean

build:
	go build -o bin/tasklease .

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

lint:
	golangci-lint run ./...

clean:
	rm -rf bin

