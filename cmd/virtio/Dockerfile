FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy the module files first to leverage Docker cache
COPY go.mod go.sum ./
COPY cmd/virtio/go.mod cmd/virtio/go.sum ./cmd/virtio/

# Download dependencies
RUN go mod download && cd cmd/virtio && go mod download

# Copy the rest of the source code
COPY . .

# Build the application
# Using -trimpath for smaller binaries and more reproducible builds
RUN cd cmd/virtio && CGO_ENABLED=0 GOOS=linux go build -x -trimpath -o /app/virtio

# Create a smaller runtime image
FROM alpine:latest

# Add only the necessary runtime dependencies
# ca-certificates for HTTPS
RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy the binary from the builder stage
COPY --from=builder /app/virtio /app/virtio

# Set environment variables
ENV PORT=8083 \
    RNG_DEVICE=/dev/hwrng \
    VIRTIO_QUEUE_SIZE=500000 \
    LOG_LEVEL=INFO

# Expose the port
EXPOSE 8083

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 -O /dev/null http://localhost:8083/health || exit 1

# Run the application
CMD ["/app/virtio"]
