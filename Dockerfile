# Multi-stage build
# Версии берутся из Makefile через --build-arg
ARG GO_VERSION=1.25.4
ARG GOOSE_VERSION=3.24.3

FROM golang:${GO_VERSION}-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache gcc musl-dev sqlite-dev git

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=1 go build -ldflags="-w -s" -o kurut-bot cmd/bot/main.go

# Install goose for migrations
ARG GOOSE_VERSION
RUN go install github.com/pressly/goose/v3/cmd/goose@v${GOOSE_VERSION}

# Final stage
FROM alpine:3.19

WORKDIR /app

# Install runtime dependencies
RUN apk add --no-cache ca-certificates sqlite procps

# Create non-root user
RUN addgroup -g 1000 appuser && \
    adduser -D -u 1000 -G appuser appuser

# Copy binary and goose from builder
COPY --from=builder /app/kurut-bot .
COPY --from=builder /go/bin/goose /usr/local/bin/goose

# Copy migrations
COPY migrations/ ./migrations/

# Create data directory
RUN mkdir -p /app/data && chown -R appuser:appuser /app

USER appuser

EXPOSE 8080

CMD ["./kurut-bot"]

