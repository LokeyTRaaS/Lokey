FROM golang:1.24-alpine AS builder

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

WORKDIR /app

# Copy the binary from the builder stage
COPY --from=builder /app/controller /app/controller

# Set environment variables
ENV PORT=8081 \
    I2C_BUS_NUMBER=1

# Expose the port
EXPOSE 8081

# Run the application
CMD ["/app/controller"]