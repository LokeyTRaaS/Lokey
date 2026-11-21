FROM golang:1.27-alpine AS builder

WORKDIR /app

RUN apk add --no-cache gcc musl-dev linux-headers

COPY go.mod go.sum ./
COPY cmd/fortuna/go.mod cmd/fortuna/go.sum* ./cmd/fortuna/

RUN go mod download && cd cmd/fortuna && go mod download

COPY . .

RUN cd cmd/fortuna && CGO_ENABLED=1 GOOS=linux go build -trimpath -o /out/fortuna

FROM alpine:latest

RUN apk --no-cache add ca-certificates libc6-compat

COPY --from=builder /out/fortuna /usr/local/bin/fortuna

ENV PORT=8082 \
    CONTROLLER_URL="http://controller:8081"

EXPOSE 8082

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD ["wget", "--no-verbose", "--tries=1", "-O", "/dev/null", "http://localhost:8082/health"]

ENTRYPOINT ["/usr/local/bin/fortuna"]
