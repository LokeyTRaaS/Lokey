FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install build dependencies for CGO and I2C
RUN apk add --no-cache gcc musl-dev linux-headers

# Install swag for Swagger documentation generation
RUN go install github.com/swaggo/swag/cmd/swag@latest

# Copy the module files first to leverage Docker cache
COPY go.mod go.sum ./
COPY cmd/api/go.mod cmd/api/go.sum* ./cmd/api/

# Download dependencies
RUN go mod download && cd cmd/api && go mod download

# Copy the rest of the source code
COPY . .

# Generate Swagger documentation
# Use absolute paths to avoid any path resolution issues
WORKDIR /app
RUN swag init --parseDependency --parseInternal \
    --generalInfo main.go \
    --dir ./cmd/api/,./pkg/api \
    --output ./pkg/api/docs

# Clean cache as this can cause issues during GO Build
RUN go clean -cache && go clean -modcache
# Build the application with CGO enabled
# Using -trimpath for smaller binaries and more reproducible builds
RUN cd cmd/api && CGO_ENABLED=1 GOOS=linux go build -x -trimpath -o /app/api

# Create a smaller runtime image
FROM alpine:latest

# Add only the necessary runtime dependencies
# ca-certificates for HTTPS, libc6-compat for dynamic linking
RUN apk --no-cache add ca-certificates libc6-compat

# Create app-bin directory for binaries (will be copied to tmpfs /app at runtime)
RUN mkdir -p /app-bin

# Copy the binary from the builder stage to /app-bin
COPY --from=builder /app/api /app-bin/api
# Copy the generated Swagger docs
COPY --from=builder /app/pkg/api/docs /app-bin/pkg/api/docs

# Create entrypoint script to copy binaries to tmpfs /app
RUN echo '#!/bin/sh' > /entrypoint.sh && \
    echo 'mkdir -p /app' >> /entrypoint.sh && \
    echo 'cp -r /app-bin/* /app/' >> /entrypoint.sh && \
    echo 'exec /app/api "$@"' >> /entrypoint.sh && \
    chmod +x /entrypoint.sh

# Create data directory
RUN mkdir -p /data

# Set environment variables
ENV PORT=8080 \
    DB_PATH="/data/api.db" \
    CONTROLLER_ADDR="http://controller:8081" \
    FORTUNA_ADDR="http://fortuna:8082"

# Expose the port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD wget --no-verbose --tries=1 -O /dev/null http://localhost:8080/api/v1/health || exit 1

# Use entrypoint to copy binaries to tmpfs /app before running
ENTRYPOINT ["/entrypoint.sh"]
CMD []