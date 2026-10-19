FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o edupilot-auth-service ./cmd/main.go

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata wget \
    && adduser -D appuser

WORKDIR /app

COPY --from=builder /app/edupilot-auth-service .

EXPOSE 8080

USER appuser

# Environment is provided at runtime (docker compose env_file), never baked into the image.
CMD ["./edupilot-auth-service"]
