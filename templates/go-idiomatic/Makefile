.PHONY: help build test test/cover vet audit clean run

## help: print this help message
help:
	@echo 'Usage:'
	@sed -n 's/^##//p' ${MAKEFILE_LIST} | column -t -s ':' | sed -e 's/^/ /'

## build: build the application
build:
	go build -o bin/study ./cmd/study/

## test: run all tests
test:
	go test -v -race ./...

## test/cover: run tests with coverage report
test/cover:
	go test -v -race -coverprofile=/tmp/coverage.out ./...
	go tool cover -html=/tmp/coverage.out

## vet: run go vet
vet:
	go vet ./...

## audit: run all quality checks
audit:
	go mod tidy -diff
	go vet ./...
	go run honnef.co/go/tools/cmd/staticcheck@latest ./...
	go test -race -vet=off ./...

## clean: remove build artifacts
clean:
	rm -rf bin/

## run: build and run
run: build
	./bin/study
