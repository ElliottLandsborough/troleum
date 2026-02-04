# Name of the Docker image
IMAGE_NAME = petroleum-app:latest

# Container name
CONTAINER_NAME = petroleum-container

# Binary name
BINARY = main

# Default target
.PHONY: run
run:
	$(MAKE) build && docker run -v $(PWD)/json:/app/json --name $(CONTAINER_NAME) -e OAUTH_CLIENT_ID -e OAUTH_CLIENT_SECRET $(IMAGE_NAME)

# Build Docker image with caching
.PHONY: build
build:
	docker build -t $(IMAGE_NAME) .

# Clean up the binary
.PHONY: clean
clean:
	rm -f $(BINARY)

# Full rebuild without cache
.PHONY: rebuildrun
rebuildrun:
	docker kill $(CONTAINER_NAME) || true
	docker rm $(CONTAINER_NAME) || true
	docker system prune -af
	$(MAKE) run