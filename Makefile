.PHONY: help test test-integration test-unit test-coverage build fmt vet lint tidy clean bench verify

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

test: ## Run all tests (requires real Azure Service Bus)
	@echo "Running all tests with real Azure Service Bus..."
	@echo "Note: Tests require AZURE_SERVICEBUS_CONNECTION_STRING in .env file"
	@echo "Get connection string from: https://portal.azure.com -> Service Bus namespace -> Shared access policies"
	go test -v ./...

test-integration: ## Run integration tests only (requires Azure Service Bus)
	@echo "Running integration tests..."
	@echo "Note: Tests require AZURE_SERVICEBUS_CONNECTION_STRING in .env file"
	go test -v -run "TestConnection|TestPublish|TestTopic|TestMessage" ./...

test-unit: ## Run unit tests only
	go test -v -short ./...

test-coverage: ## Run tests with coverage
	go test -cover -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

bench: ## Run benchmark tests
	go test -bench=. -benchmem ./...

build: ## Build the project
	go build ./...

fmt: ## Format code
	go fmt ./...

vet: ## Run go vet
	go vet ./...

lint: ## Run golangci-lint (requires golangci-lint to be installed)
	golangci-lint run

tidy: ## Tidy go modules
	go mod tidy

clean: ## Clean up test artifacts
	rm -f coverage.out coverage.html
	@echo "Cleaned up test artifacts"

verify: fmt vet ## Run formatting and vetting

.DEFAULT_GOAL := help
