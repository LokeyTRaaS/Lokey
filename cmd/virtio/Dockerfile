FROM golang:1.27-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
COPY cmd/virtio/go.mod cmd/virtio/go.sum ./cmd/virtio/

RUN go mod download && cd cmd/virtio && go mod download

COPY . .

RUN cd cmd/virtio && CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/virtio

FROM alpine:latest

RUN apk --no-cache add ca-certificates

COPY --from=builder /out/virtio /usr/local/bin/virtio

ENV PORT=8083 \
    VIRTIO_QUEUE_SIZE=500000 \
    LOG_LEVEL=INFO

EXPOSE 8083

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD ["wget", "--no-verbose", "--tries=1", "-O", "/dev/null", "http://localhost:8083/health"]

ENTRYPOINT ["/usr/local/bin/virtio"]
