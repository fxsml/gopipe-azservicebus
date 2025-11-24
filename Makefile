.PHONY: help emulator-start emulator-stop emulator-restart emulator-logs emulator-status test test-integration test-unit clean claude-build claude-start claude-stop claude-remove claude-restart claude-attach claude-shell claude-status claude-logs

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

claude-build: ## Build Claude Code Docker image
	@echo "Building Claude Code Docker image..."
	docker build -f Dockerfile.claude -t gopipe-azservicebus-claude:latest .
	@echo "Image built successfully."

claude-start: claude-build ## Start Claude Code container (idempotent)
	@echo "Starting Claude Code container..."
	@if [ "$$(docker ps -aq -f name=gopipe-azservicebus-claude)" ]; then \
		echo "Container exists. Checking status..."; \
		if [ "$$(docker ps -q -f name=gopipe-azservicebus-claude)" ]; then \
			echo "Container is already running."; \
		else \
			echo "Starting existing container..."; \
			docker start gopipe-azservicebus-claude; \
		fi; \
	else \
		echo "Creating new container..."; \
		docker run -d --name gopipe-azservicebus-claude \
			-v /var/run/docker.sock:/var/run/docker.sock \
			-e ANTHROPIC_API_KEY=$${ANTHROPIC_API_KEY:-} \
			gopipe-azservicebus-claude:latest; \
		echo "Cloning repository..."; \
		docker exec gopipe-azservicebus-claude bash -c "git clone https://github.com/fxsml/gopipe-azservicebus.git /workspace/repo 2>/dev/null || echo 'Repo already exists'"; \
	fi
	@echo ""
	@echo "Container is running. Use 'make claude-attach' to connect."
	@echo "To set API key: docker exec gopipe-azservicebus-claude bash -c 'export ANTHROPIC_API_KEY=your_key && claude login'"

claude-stop: ## Stop Claude Code container (keeps container for restart)
	@echo "Stopping Claude Code container..."
	@docker stop gopipe-azservicebus-claude 2>/dev/null || echo "Container not running"
	@echo "Container stopped (use 'make claude-start' to restart or 'make claude-remove' to delete)"

claude-remove: ## Remove Claude Code container completely
	@echo "Removing Claude Code container..."
	@docker stop gopipe-azservicebus-claude 2>/dev/null || true
	@docker rm gopipe-azservicebus-claude 2>/dev/null || true
	@echo "Container removed."

claude-restart: claude-stop claude-start ## Restart Claude Code container

claude-attach: ## Attach to interactive shell in workspace
	@echo "Attaching to Claude Code container..."
	@if [ ! "$$(docker ps -q -f name=gopipe-azservicebus-claude)" ]; then \
		echo "Container is not running. Starting..."; \
		$(MAKE) claude-start; \
	fi
	@docker exec -it gopipe-azservicebus-claude bash -c "cd /workspace/repo && exec bash"

claude-shell: ## Open interactive shell in container root
	@echo "Opening shell in Claude Code container..."
	@if [ ! "$$(docker ps -q -f name=gopipe-azservicebus-claude)" ]; then \
		echo "Container is not running. Starting..."; \
		$(MAKE) claude-start; \
	fi
	@docker exec -it gopipe-azservicebus-claude /bin/bash

claude-status: ## Show Claude Code container status
	@echo "Claude Code Container Status:"
	@echo "----------------------------"
	@if [ "$$(docker ps -q -f name=gopipe-azservicebus-claude)" ]; then \
		echo "Status: RUNNING"; \
		docker ps -f name=gopipe-azservicebus-claude --format "ID: {{.ID}}\nUptime: {{.Status}}\nImage: {{.Image}}"; \
	elif [ "$$(docker ps -aq -f name=gopipe-azservicebus-claude)" ]; then \
		echo "Status: STOPPED"; \
		docker ps -a -f name=gopipe-azservicebus-claude --format "ID: {{.ID}}\nStatus: {{.Status}}\nImage: {{.Image}}"; \
	else \
		echo "Status: NOT CREATED"; \
	fi

claude-logs: ## Show Claude Code container logs
	@docker logs gopipe-azservicebus-claude 2>&1 || echo "Container not found"

verify: fmt vet ## Run formatting and vetting

.DEFAULT_GOAL := help
