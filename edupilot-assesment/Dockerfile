FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o edupilot-assesment ./cmd/main.go

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -H appuser

WORKDIR /app

COPY --from=builder /app/edupilot-assesment .

EXPOSE 8080

USER appuser

CMD ["./edupilot-assesment"]
