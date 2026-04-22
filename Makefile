# Name of the Docker image
IMAGE_NAME = troleum-app:latest

# Remote host architecture (current server is amd64)
REMOTE_PLATFORM = linux/amd64

# Remote host user UID:GID for the deploy user
REMOTE_UID = 1001:1001

# App container name
APP_CONTAINER_NAME = troleum_app

# Binary name
BINARY = main

# Build-time cache bust token for static assets in production images
ASSET_VERSION ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)-$(shell date +%Y%m%d%H%M%S)

# Default target - use Docker Compose
.PHONY: run
run:
	docker-compose up -d

# Build with Docker Compose
.PHONY: build
build:
	docker-compose build

# Run application tests locally before packaging or deploy steps
.PHONY: test
test:
	go test ./app -count=1

# Clean up the binary and containers
.PHONY: clean
clean:
	rm -f $(BINARY)
	docker-compose down || true
	docker rm -f $(APP_CONTAINER_NAME) || true

# Full rebuild without cache
.PHONY: rebuildrun
rebuildrun:
	$(MAKE) test
	docker-compose down || true
	docker system prune -af
	docker rmi -f $(IMAGE_NAME) || true
	docker rm -f $(APP_CONTAINER_NAME) || true
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
	docker logs -f troleum_app

# Stop all services
.PHONY: stop
stop:
	docker-compose down

# prod only commands:
deploy-to-production: test save-image send-image run-remote

# save docker image to file for distribution
.PHONY: save-image
save-image:
	$(MAKE) build-remote-image
	docker save $(IMAGE_NAME) -o troleum_image.tar

.PHONY: build-remote-image
build-remote-image:
	docker buildx build --platform $(REMOTE_PLATFORM) --build-arg ASSET_VERSION=$(ASSET_VERSION) --load -t $(IMAGE_NAME) .

.PHONY: send-env
send-env:
	ssh troleumdeploy "mkdir -p /home/deploy/troleum && chmod 700 /home/deploy/troleum"
	scp .env.prod troleumdeploy:/home/deploy/troleum/.env
	ssh troleumdeploy "chmod 600 /home/deploy/troleum/.env"

# send docker image to remote server over scp
.PHONY: send-image
send-image:
	ssh troleumdeploy "mkdir -p /home/deploy/troleum && chmod 700 /home/deploy/troleum"
	scp troleum_image.tar troleumdeploy:/home/deploy/troleum/troleum_image.tar
	ssh troleumdeploy "mkdir -p /home/deploy/troleum/json && chmod 700 /home/deploy/troleum/json"
	ssh troleumdeploy "chmod 600 /home/deploy/troleum/.env /home/deploy/troleum/troleum_image.tar"

# execute image on remote server
.PHONY: run-remote
run-remote:
	ssh troleumdeploy "docker rm -f troleum_app || true"
	ssh troleumdeploy "docker load -i /home/deploy/troleum/troleum_image.tar && docker rm -f $(APP_CONTAINER_NAME) || true && docker run --user $(REMOTE_UID) -d --restart always --platform $(REMOTE_PLATFORM) -p 8080:8080 -v /home/deploy/troleum/json:/app/json:Z --name $(APP_CONTAINER_NAME) --env-file /home/deploy/troleum/.env $(IMAGE_NAME)"
	ssh troleumdeploy "rm -f /home/deploy/troleum/troleum_image.tar"
	ssh troleumdeploy "docker logs -f $(APP_CONTAINER_NAME)"