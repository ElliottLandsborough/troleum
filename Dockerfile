# Use an official Go image as the builder
FROM golang:1.25-alpine AS builder

# Create app directory
WORKDIR /app

# Copy go.mod and go.sum first to cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY app/api.go /app/
COPY app/main.go /app/
COPY app/storage.go /app/
COPY app/web.go /app/

RUN ls -alh /app

# Build the binary (without debug optimizations)
#RUN go build -o main .

# Build the binary with production optimizations
ENV CGO_ENABLED=0
RUN go build -ldflags="-s -w" -trimpath -o main .

# Use a minimal image to run the binary safely
FROM alpine:latest
WORKDIR /app

# Create json directory for data persistence
RUN mkdir -p json

# Copy the binary from the builder
COPY --from=builder /app/main .

# Copy the static files (if any)
COPY static ./static

# Run the binary
CMD ["./main"]
