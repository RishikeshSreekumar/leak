.PHONY: build test vet fmt fmt-check cover check tidy install

build:
	go build -o leak .

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

# Fails if anything is unformatted — the same gate CI applies.
fmt-check:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "These files are not gofmt-ed:"; echo "$$unformatted"; exit 1; \
	fi

cover:
	go test ./... -coverprofile=coverage.out
	go tool cover -func=coverage.out | tail -1

# Run everything CI runs, in the same order.
check: fmt-check vet test

tidy:
	go mod tidy

install:
	go install .
