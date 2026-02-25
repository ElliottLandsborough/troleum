# Dev with more debug packages
FROM golang:1.26 AS builder

# Produce a smaller image for production
#FROM golang:1.26-alpine AS builder

# Create app directory
WORKDIR /app

# Copy go.mod and go.sum first to cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY app/config.go /app/
COPY app/database.go /app/
COPY app/debug.go /app/
COPY app/govApi.go /app/
COPY app/json.go /app/
COPY app/main.go /app/
COPY app/memory.go /app/
COPY app/nodebug.go /app/
COPY app/prices.go /app/
COPY app/queue.go /app/
COPY app/stations.go /app/
COPY app/webHandlers.go /app/
COPY app/webServer.go /app/

RUN ls -alh /app

# Build the binary (without debug optimizations)
RUN go build -x -v -gcflags=all=-d=checkptr=1 -race -tags debug -o main .

# Build the binary with production optimizations
#ENV CGO_ENABLED=0
#RUN go build -ldflags="-s -w" -trimpath -o main .

# Use a minimal image to run the binary safely
FROM alpine:latest

# Create non-root user
RUN addgroup -g 1000 appuser && \
    adduser -D -u 1000 -G appuser appuser

WORKDIR /app

# Create json directory for data persistence
RUN mkdir -p json

# Copy binary and set permissions (read/execute only)
COPY --from=builder --chown=appuser:appuser --chmod=555 /app/main .

# Copy static/web files (read-only)
COPY --chown=appuser:appuser --chmod=555 static ./static

# Create json directory with write permissions for data persistence
RUN mkdir -p json && chown appuser:appuser json && chmod 755 json

# Run the binary
CMD ["./main"]
