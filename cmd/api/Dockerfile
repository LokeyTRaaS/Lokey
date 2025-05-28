FROM golang:1.24-alpine AS builder

WORKDIR /app

# Copy the module files first to leverage Docker cache
COPY go.mod go.sum ./
COPY cmd/api/go.mod cmd/api/go.sum* ./cmd/api/

# Download dependencies
RUN go mod download && cd cmd/api && go mod download

# Copy the rest of the source code
COPY . .

# Build the application
RUN cd cmd/api && CGO_ENABLED=1 GOOS=linux go build -o /app/api

# Create a smaller runtime image
FROM alpine:latest

RUN apk --no-cache add ca-certificates libc6-compat

WORKDIR /app

# Copy the binary from the builder stage
COPY --from=builder /app/api /app/api

# Create data directory
RUN mkdir -p /data

# Set environment variables
ENV PORT=8080 \
    DB_PATH="/data/api.db" \
    CONTROLLER_ADDR="http://controller:8081" \
    FORTUNA_ADDR="http://fortuna:8082" \
    TRNG_QUEUE_SIZE=100 \
    FORTUNA_QUEUE_SIZE=100

# Expose the port
EXPOSE 8080

# Run the application
CMD ["/app/api"]