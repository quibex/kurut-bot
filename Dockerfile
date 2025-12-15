# Multi-stage build
ARG GO_VERSION=1.25.4
ARG GOOSE_VERSION=3.24.3

FROM golang:${GO_VERSION}-alpine AS builder

WORKDIR /app

RUN apk add --no-cache gcc musl-dev sqlite-dev git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=1 go build -ldflags="-w -s" -o kurut-bot cmd/bot/main.go

ARG GOOSE_VERSION
RUN go install github.com/pressly/goose/v3/cmd/goose@v${GOOSE_VERSION}

# Final stage
FROM alpine:3.19

WORKDIR /app

RUN apk add --no-cache ca-certificates sqlite procps

COPY --from=builder /app/kurut-bot .
COPY --from=builder /go/bin/goose /usr/local/bin/goose
COPY migrations/ ./migrations/

RUN mkdir -p /app/data

EXPOSE 8080

CMD ["./kurut-bot"]
