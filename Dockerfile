FROM golang:1.26-bookworm

WORKDIR /src

# System deps: ffmpeg for audio/sticker/media processing, ca-certificates for TLS.
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates ffmpeg curl \
    && rm -rf /var/lib/apt/lists/*

# Node.js 20 LTS for the edge-tts TTS CLI.
RUN curl -fsSL https://deb.nodesource.com/setup_20.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/*

# edge-tts CLI used by the !tts plugin (text-to-speech synthesis).
RUN npm install -g edge-tts

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /app/sonicos ./cmd/bot

WORKDIR /app
RUN mkdir -p /app/data

ENV DATABASE_DRIVER=sqlite3
ENV DATABASE_URL=file:./data/sonicos.db?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000
ENV REDIS_URL=redis://localhost:6379/0
ENV BOT_PREFIX=!
ENV WORKERS=8
ENV QUEUE_SIZE=512
ENV LOG_LEVEL=info
ENV FFMPEG_PATH=ffmpeg

CMD ["/app/sonicos"]
