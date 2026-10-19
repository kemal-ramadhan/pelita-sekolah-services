# Pinned to the Go version declared in go.mod
FROM golang:1.26.2-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

# .env is excluded via .dockerignore; config comes from env_file at runtime
COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /app/gateway-service ./cmd/gateway

FROM alpine:3.22

RUN apk add --no-cache tzdata ca-certificates \
    && addgroup -S app && adduser -S -G app app

WORKDIR /app
COPY --from=builder /app/gateway-service .

# Non-root can bind :80 because Docker sets net.ipv4.ip_unprivileged_port_start=0 (Engine >= 20.10).
USER app

EXPOSE 80

CMD ["./gateway-service"]
