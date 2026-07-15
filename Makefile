.PHONY: build test vet fmt tidy install

build:
	go build -o leak .

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

tidy:
	go mod tidy

install:
	go install .
