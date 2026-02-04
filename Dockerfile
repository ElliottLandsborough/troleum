# Use an official Go image as the builder
FROM golang:1.25-alpine AS builder

# Create app directory
WORKDIR /app

# Copy go.mod and go.sum first to cache dependencies
#COPY go.mod go.sum ./
COPY go.mod ./
#RUN go mod download

# Copy the rest of the source code
COPY app/main.go /app/
COPY app/storage.go /app/

RUN ls -alh /app

# Build the binary
RUN go build -o main .

# Use a minimal image to run the binary safely
FROM alpine:latest
WORKDIR /app

# Copy the binary from the builder
COPY --from=builder /app/main .

# Run the binary
CMD ["./main"]
