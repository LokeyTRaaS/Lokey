FROM golang:1.27-alpine AS builder

WORKDIR /app

# Install build dependencies for CGO and I2C
RUN apk add --no-cache gcc musl-dev linux-headers

# Copy the module files first to leverage Docker cache
COPY go.mod go.sum ./
COPY cmd/api/go.mod cmd/api/go.sum* ./cmd/api/

# Download dependencies
RUN go mod download && cd cmd/api && go mod download

# Copy the rest of the source code (includes committed pkg/api/docs from swag)
COPY . .

# Swagger docs are generated in-repo (pkg/api/docs). Rebuild them with:
#   docker run --rm -v "$PWD":/app -w /app golang:1.27-alpine \
#     sh -c 'go install github.com/swaggo/swag/cmd/swag@v1.16.4 &&
#            swag init --parseDependency --parseInternal --generalInfo main.go --dir ./cmd/api/,./pkg/api --output ./pkg/api/docs'
RUN test -f ./pkg/api/docs/docs.go && test -f ./pkg/api/docs/swagger.json

# Build the application with CGO enabled
WORKDIR /app
RUN cd cmd/api && CGO_ENABLED=1 GOOS=linux go build -trimpath -o /out/api

FROM alpine:latest

RUN apk --no-cache add ca-certificates libc6-compat

COPY --from=builder /out/api /usr/local/bin/api

ENV PORT=8080 \
    CONTROLLER_ADDR="http://controller:8081" \
    FORTUNA_ADDR="http://fortuna:8082"

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD ["wget", "--no-verbose", "--tries=1", "-O", "/dev/null", "http://localhost:8080/api/v1/health"]

ENTRYPOINT ["/usr/local/bin/api"]
