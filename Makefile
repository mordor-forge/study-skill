.PHONY: help setup test test-catalog test-fsrs test-e2e validate lint typecheck coverage build clean

help:
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z0-9_-]+:.*## / {printf "%-14s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

setup: ## Install development dependencies for local checks
	cd scripts/catalog && uv sync --all-groups

test: validate test-catalog test-fsrs test-e2e ## Run all deterministic tests

validate: ## Validate skill metadata and repository support files
	cd scripts/catalog && uv run python ../../scripts/e2e/validate-skill.py ../../SKILL.md

test-catalog: ## Run Python catalog tests
	cd scripts/catalog && uv run pytest

test-fsrs: ## Run Go FSRS tests
	cd scripts/fsrs && go test ./...

test-e2e: ## Run the study workspace lifecycle smoke test
	scripts/e2e/study-lifecycle-smoke.sh

lint: ## Run linters
	cd scripts/catalog && uv run ruff check src tests
	cd scripts/fsrs && test -z "$$(gofmt -l .)"

typecheck: ## Run static type checks
	cd scripts/catalog && uv run pyright src

coverage: ## Run tests with coverage thresholds
	cd scripts/catalog && uv run pytest --cov=src/catalog --cov-report=term-missing --cov-report=xml
	cd scripts/fsrs && go test -coverprofile=coverage.out -covermode=atomic ./...

build: ## Build the FSRS scheduler binary
	cd scripts/fsrs && go build -o fsrs ./cmd/fsrs/

clean: ## Remove generated local artifacts
	rm -rf scripts/catalog/.pytest_cache scripts/catalog/.ruff_cache scripts/catalog/.coverage
	rm -f scripts/fsrs/fsrs
