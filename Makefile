# Name of the Docker image
IMAGE_NAME = petroleum-app:latest

# Binary name
BINARY = main

# Default target
.PHONY: run
run:
	$(MAKE) build && docker run -v $(PWD)/json:/app/json -e OAUTH_CLIENT_ID -e OAUTH_CLIENT_SECRET $(IMAGE_NAME)

# Build Docker image with caching
.PHONY: build
build:
	docker build -t $(IMAGE_NAME) .

# Clean up the binary
.PHONY: clean
clean:
	rm -f $(BINARY)