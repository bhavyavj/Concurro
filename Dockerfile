# Multi-stage build for small production-like image
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Cache deps
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /concurro ./cmd/api

# Final minimal image
FROM alpine:3.20

WORKDIR /app

# ca-certificates for HTTPS fetches by workers
RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /concurro /app/concurro

# Default DB location inside container
ENV CONCURRO_DB_PATH=/app/concurro.db
ENV CONCURRO_ADDR=:8080

EXPOSE 8080

ENTRYPOINT ["/app/concurro", "serve"]
