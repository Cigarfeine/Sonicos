FROM golang:1.26-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /out/sonicos ./cmd/bot

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=build /out/sonicos /app/sonicos
RUN mkdir -p /app/data
ENV DATABASE_DRIVER=sqlite3
ENV DATABASE_URL=file:./data/sonicos.db?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000
ENV BOT_PREFIX=!
ENV WORKERS=8
ENV QUEUE_SIZE=512
ENV LOG_LEVEL=info
CMD ["/app/sonicos"]
