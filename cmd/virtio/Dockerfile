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

# Create app-bin directory for binaries (will be copied to tmpfs /app at runtime)
RUN mkdir -p /app-bin

# Copy the binary from the builder stage to /app-bin
COPY --from=builder /app/virtio /app-bin/virtio

# Create entrypoint script to copy binaries to tmpfs /app
RUN echo '#!/bin/sh' > /entrypoint.sh && \
    echo 'mkdir -p /app' >> /entrypoint.sh && \
    echo 'cp -r /app-bin/* /app/' >> /entrypoint.sh && \
    echo 'exec /app/virtio "$@"' >> /entrypoint.sh && \
    chmod +x /entrypoint.sh

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

# Use entrypoint to copy binaries to tmpfs /app before running
ENTRYPOINT ["/entrypoint.sh"]
CMD []
