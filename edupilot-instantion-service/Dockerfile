FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o edupilot-instantion-service ./cmd/main.go

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -H appuser

WORKDIR /app

COPY --from=builder /app/edupilot-instantion-service .

# Configuration comes from env_file / environment at runtime (never baked into the image).
EXPOSE 8080

USER appuser

CMD ["./edupilot-instantion-service"]
