FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install build dependencies for CGO and I2C
RUN apk add --no-cache gcc musl-dev linux-headers

# Copy the module files first to leverage Docker cache
COPY go.mod go.sum ./
COPY cmd/controller/go.mod cmd/controller/go.sum ./cmd/controller/

# Download dependencies
RUN go mod download && cd cmd/controller && go mod download

# Copy the rest of the source code
COPY . .

# Build the application with CGO enabled
# Using -trimpath for smaller binaries and more reproducible builds
RUN cd cmd/controller && CGO_ENABLED=1 GOOS=linux go build -x -trimpath -o /app/controller

# Create a smaller runtime image
FROM alpine:latest

# Add only the necessary runtime dependencies
# ca-certificates for HTTPS, libc6-compat for dynamic linking, i2c-tools for I2C support
RUN apk --no-cache add ca-certificates libc6-compat i2c-tools

# Create app-bin directory for binaries (will be copied to tmpfs /app at runtime)
RUN mkdir -p /app-bin

# Copy the binary from the builder stage to /app-bin
COPY --from=builder /app/controller /app-bin/controller

# Create entrypoint script to copy binaries to tmpfs /app
RUN echo '#!/bin/sh' > /entrypoint.sh && \
    echo 'mkdir -p /app' >> /entrypoint.sh && \
    echo 'cp -r /app-bin/* /app/' >> /entrypoint.sh && \
    echo 'exec /app/controller "$@"' >> /entrypoint.sh && \
    chmod +x /entrypoint.sh

# Set environment variables
ENV PORT=8081 \
    I2C_BUS_NUMBER=1 \
    FORCE_CONFIG=false \
    LOG_LEVEL=INFO

# Expose the port
EXPOSE 8081

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 -O /dev/null http://localhost:8081/health || exit 1

# Use entrypoint to copy binaries to tmpfs /app before running
ENTRYPOINT ["/entrypoint.sh"]
CMD []