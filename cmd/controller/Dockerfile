FROM golang:1.24-alpine AS builder

WORKDIR /app

# Copy the module files first to leverage Docker cache
COPY go.mod go.sum ./
COPY cmd/controller/go.mod cmd/controller/go.sum ./cmd/controller/

# Download dependencies
RUN go mod download && cd cmd/controller && go mod download

# Copy the rest of the source code
COPY . .

# Build the application
RUN cd cmd/controller && CGO_ENABLED=1 GOOS=linux go build -o /app/controller

# Create a smaller runtime image
FROM alpine:latest

RUN apk --no-cache add ca-certificates libc6-compat

WORKDIR /app

# Copy the binary from the builder stage
COPY --from=builder /app/controller /app/controller

# Create data directory
RUN mkdir -p /data

# Set environment variables
ENV PORT=8081 \
    I2C_BUS_NUMBER=1 \
    DB_PATH="/data/trng.db" \
    HASH_INTERVAL_MS=1000 \
    TRNG_QUEUE_SIZE=100

# Expose the port
EXPOSE 8081

# Run the application
CMD ["/app/controller"]