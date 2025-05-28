FROM golang:1.24-alpine AS builder

WORKDIR /app

# Copy the module files first to leverage Docker cache
COPY go.mod go.sum ./
COPY cmd/fortuna/go.mod cmd/fortuna/go.sum* ./cmd/fortuna/

# Download dependencies
RUN go mod download && cd cmd/fortuna && go mod download

# Copy the rest of the source code
COPY . .

# Build the application
RUN cd cmd/fortuna && CGO_ENABLED=1 GOOS=linux go build -o /app/fortuna

# Create a smaller runtime image
FROM alpine:latest

RUN apk --no-cache add ca-certificates libc6-compat

WORKDIR /app

# Copy the binary from the builder stage
COPY --from=builder /app/fortuna /app/fortuna

# Create data directory
RUN mkdir -p /data

# Set environment variables
ENV PORT=8082 \
    DB_PATH="/data/fortuna.db" \
    CONTROLLER_URL="http://controller:8081" \
    PROCESS_INTERVAL_MS=5000 \
    FORTUNA_QUEUE_SIZE=100 \
    AMPLIFICATION_FACTOR=4 \
    SEED_COUNT=3

# Expose the port
EXPOSE 8082

# Run the application
CMD ["/app/fortuna"]
