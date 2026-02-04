# Name of the Docker image
IMAGE_NAME = 'golang:latest'

# Binary name
BINARY = main

# Default target
.PHONY: run
run: build
	docker run --rm $(IMAGE_NAME)

# Build Docker image with caching
.PHONY: build
build: $(BINARY)

# Build Go binary locally inside Docker builder
$(BINARY):
	docker run --rm -v "$(PWD)":/app -w /app golang:1.25-alpine sh -c "go mod download && go build -o $(BINARY) . " && go run main.go

# Clean up the binary
.PHONY: clean
clean:
	rm -f $(BINARY)