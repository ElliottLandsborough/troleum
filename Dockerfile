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
COPY app/stats.go /app/
COPY app/webHandlers.go /app/
COPY app/webServer.go /app/

# Build the binary (with debug optimizations)
#RUN go build -x -v -gcflags=all=-d=checkptr=1 -race -tags debug -o troleum-go .

# Build the binary with production optimizations
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG GOAMD64=v3
RUN set -eux; \
    export CGO_ENABLED=0; \
    export GOOS="${TARGETOS}"; \
    export GOARCH="${TARGETARCH}"; \
    if [ "${GOARCH}" = "amd64" ]; then \
        export GOAMD64="${GOAMD64}"; \
    fi; \
    go build -ldflags="-s -w" -trimpath -o troleum-go .

# Prepare runtime assets and stamped static files.
FROM alpine:3 AS runtime-prep
ARG ASSET_VERSION=dev

WORKDIR /app

# Copy binary and data files into a prepared filesystem tree.
COPY --from=builder /app/troleum-go /app/troleum-go

# Copy OSM-derived UK boundary data used for coordinate correction
COPY app/uk_land_osm.json ./uk_land_osm.json

# Copy image assets (read-only, not executable)
COPY assets/favicon.ico ./assets/favicon.ico
COPY assets/favicon-16x16.png ./assets/favicon-16x16.png
COPY assets/favicon-32x32.png ./assets/favicon-32x32.png
COPY assets/favicon-48x48.png ./assets/favicon-48x48.png
COPY assets/apple-touch-icon-180x180.png ./assets/apple-touch-icon-180x180.png
COPY assets/apple-touch-icon.png ./assets/apple-touch-icon.png
COPY assets/android-chrome-192x192.png ./assets/android-chrome-192x192.png
COPY assets/android-chrome-512x512.png ./assets/android-chrome-512x512.png

# Copy static/web files (read-only)
COPY static ./static

# Stamp static asset URLs at build time and pre-create the writable data directory.
RUN set -eux; \
    sed -i "s/__ASSET_VERSION__/${ASSET_VERSION}/g" /app/static/index.html; \
    mkdir -p /app/json; \
    chmod 555 /app/troleum-go; \
    chmod 444 /app/uk_land_osm.json; \
    find /app/assets -type d -exec chmod 555 {} \;; \
    find /app/assets -type f -exec chmod 444 {} \;; \
    find /app/static -type d -exec chmod 555 {} \;; \
    find /app/static -type f -exec chmod 444 {} \;; \
    chmod 777 /app/json

# Use a minimal runtime image with a built-in non-root user.
FROM gcr.io/distroless/static-debian13:nonroot AS runtime

WORKDIR /app

COPY --from=runtime-prep /app /app

EXPOSE 8080
ENTRYPOINT ["/app/troleum-go"]
