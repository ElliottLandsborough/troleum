# Name of the Docker image
IMAGE_NAME = petroleum:latest

# App container name
APP_CONTAINER_NAME = petroleum_app

# Container name
POSTGRES_CONTAINER_NAME = petroleum_postgres

# Binary name
BINARY = main

# Default target - use Docker Compose
.PHONY: run
run:
	docker-compose up -d

# Build and run with standalone Docker (alternative to compose)
.PHONY: standalone
standalone:
	$(MAKE) build-standalone && docker run -p 8080:8080 -v $(PWD)/json:/app/json --name $(APP_CONTAINER_NAME)_standalone -e OAUTH_CLIENT_ID -e OAUTH_CLIENT_SECRET $(IMAGE_NAME)

# Build Docker image standalone (without compose)
.PHONY: build-standalone
build-standalone:
	docker build -t $(IMAGE_NAME) .

# Build with Docker Compose
.PHONY: build
build:
	docker-compose build

# Clean up the binary and containers
.PHONY: clean
clean:
	rm -f $(BINARY)
	docker-compose down || true
	docker kill $(APP_CONTAINER_NAME)_standalone || true
	docker rm -f $(APP_CONTAINER_NAME)_standalone || true

# Full rebuild without cache
.PHONY: rebuildrun
rebuildrun:
	docker-compose down || true
	docker system prune -af
	docker kill $(APP_CONTAINER_NAME) || true
	docker rmi -f $(IMAGE_NAME) || true
	docker rm -f $(APP_CONTAINER_NAME) || true
	docker kill $(APP_CONTAINER_NAME)_standalone || true
	docker rm -f $(APP_CONTAINER_NAME)_standalone || true
	docker kill $(POSTGRES_CONTAINER_NAME) || true
	docker rm -f $(POSTGRES_CONTAINER_NAME) || true
	$(MAKE) clean
	$(MAKE) run
	$(MAKE) logs-app

# View logs
.PHONY: logs
logs:
	docker-compose logs -f

# View app logs only
.PHONY: logs-app
logs-app:
	docker logs -f petroleum_app

# Stop all services
.PHONY: stop
stop:
	docker-compose down