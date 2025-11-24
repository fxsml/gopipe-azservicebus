.PHONY: help emulator-start emulator-stop emulator-restart emulator-logs emulator-status test test-integration test-unit clean

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

emulator-start: ## Start the Service Bus emulator
	@echo "Starting Azure Service Bus emulator..."
	docker compose up -d
	@echo "Waiting for emulator to be ready..."
	@sleep 5
	@make emulator-status

emulator-stop: ## Stop the Service Bus emulator
	@echo "Stopping Azure Service Bus emulator..."
	docker compose down

emulator-restart: emulator-stop emulator-start ## Restart the Service Bus emulator

emulator-logs: ## Show emulator logs
	docker compose logs -f

emulator-status: ## Check emulator status
	@echo "Checking emulator status..."
	@docker compose ps
	@echo ""
	@echo "Service Bus emulator should be available at: http://localhost:5678"
	@echo "SQL Server should be available at: localhost:1433"

test: ## Run all tests
	go test -v ./...

test-integration: ## Run integration tests (requires emulator)
	@echo "Running integration tests..."
	@echo "Note: Make sure the emulator is running with 'make emulator-start'"
	go test -v -run "TestEmulator" ./...

test-unit: ## Run unit tests only
	go test -v -short ./...

test-coverage: ## Run tests with coverage
	go test -cover -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

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

clean: ## Clean up
	rm -f coverage.out coverage.html
	docker compose down -v
	@echo "Cleaned up test artifacts and stopped emulator"

verify: fmt vet ## Run formatting and vetting

.DEFAULT_GOAL := help
