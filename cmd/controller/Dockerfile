FROM golang:1.27-alpine AS builder

WORKDIR /app

RUN apk add --no-cache gcc musl-dev linux-headers

COPY go.mod go.sum ./
COPY cmd/controller/go.mod cmd/controller/go.sum ./cmd/controller/

RUN go mod download && cd cmd/controller && go mod download

COPY . .

RUN cd cmd/controller && CGO_ENABLED=1 GOOS=linux go build -trimpath -o /out/controller

FROM alpine:latest

RUN apk --no-cache add ca-certificates libc6-compat i2c-tools wget

COPY --from=builder /out/controller /usr/local/bin/controller

ENV PORT=8081 \
    I2C_BUS_NUMBER=1 \
    FORCE_CONFIG=false \
    LOG_LEVEL=INFO

EXPOSE 8081

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD ["wget", "--no-verbose", "--tries=1", "-O", "/dev/null", "http://localhost:8081/health"]

ENTRYPOINT ["/usr/local/bin/controller"]
