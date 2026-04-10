# Name of the Docker image
IMAGE_NAME = petroleum:latest

# App container name
APP_CONTAINER_NAME = petroleum_app

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

# prod only commands:

# save docker image to file for distribution
.PHONY: save-image
save-image:
	docker save -o petroleum_image.tar $(IMAGE_NAME)

# send docker image to remote server over scp
.PHONY: send-image
send-image:
	scp petroleum_image.tar petroleum:/root/petroleum_image.tar

# execute image on remote server
.PHONY: run-remote
run-remote:
	ssh petroleum "docker load -i /root/petroleum_image.tar && docker run -d -p 8080:8080 -v /root/json:/app/json --name petroleum_app -e OAUTH_CLIENT_ID -e OAUTH_CLIENT_SECRET $(IMAGE_NAME)"