.PHONY: all build test test-fixtures test-matrix test-matrix-local install clean

BINARY=mutate
CMD=./cmd/mutate

all: build

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(BINARY) $(CMD)

test:
	go test -v ./...

test-fixtures:
	go run tests/fixtures/generate_fixtures.go tests/fixtures

test-matrix: test-fixtures
	./tests/os_matrix/run_matrix.sh

test-matrix-local: test-fixtures
	./tests/os_matrix/run_matrix.sh --mode local

install:
	./install.sh --system

clean:
	rm -f $(BINARY) mut
	rm -rf tests/fixtures/sample*
