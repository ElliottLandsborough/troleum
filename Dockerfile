# Dev with more debug packages
#FROM golang:1.26 AS builder

# Produce a smaller image for production
FROM golang:1.26-alpine AS builder

# Create app directory
WORKDIR /app

# Copy go.mod and go.sum first to cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY app/config.go /app/
COPY app/coordinates.go /app/
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

# Build the binary (with debug optimizations)
#RUN go build -x -v -gcflags=all=-d=checkptr=1 -race -tags debug -o main .

# Build the binary with production optimizations
ENV CGO_ENABLED=0
RUN go build -ldflags="-s -w" -trimpath -o main .

# Use a minimal image to run the binary safely
# PROD
FROM alpine:latest
# DEV - we need debian for debugging tools and to avoid musl issues
#FROM debian:bookworm-slim

# Create a non-root user to run the app securely (alpine syntax)
RUN addgroup -g 1000 appuser && \
    adduser -D -u 1000 -G appuser appuser

# Create non-root user (Debian syntax)
#RUN groupadd -g 1000 appuser && \
#    useradd -u 1000 -g appuser -s /bin/bash -m appuser
# Install CA certificates for HTTPS requests (Debian only, not needed in alpine since it's included in the base image)
#RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Create json directory for data persistence
RUN mkdir -p json

# Copy binary and set permissions (read/execute only)
COPY --from=builder --chown=appuser:appuser --chmod=555 /app/main .

# Copy OSM-derived UK boundary data used for coordinate correction
COPY --chown=appuser:appuser --chmod=444 app/uk_land_osm.json ./uk_land_osm.json

# Copy static/web files (read-only)
COPY --chown=appuser:appuser --chmod=555 static ./static

# Ensure runtime paths are writable by the non-root user
RUN mkdir -p /app/json && chown -R appuser:appuser /app && chmod 755 /app/json

# Run as non-root user
USER appuser:appuser

# Run the binary
CMD ["./main"]
