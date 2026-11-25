# Sonicos WhatsApp Bot

A Go WhatsApp bot built with whatsmeow. It stores WhatsApp session credentials in SQLite (or PostgreSQL), uses Redis to suppress duplicate command processing, and loads commands as separate Go plugins.

[![Deploy on Railway](https://railway.com/button.svg)](https://railway.com/deploy?repo=github.com/SafwanGanz/Sonicos)
[![Deploy to Heroku](https://www.herokucdn.com/deploy/button.svg)](https://heroku.com/deploy?template=https%3A%2F%2Fgithub.com%2FSafwanGanz%2FSonicos)

> The deploy buttons target `https://github.com/SafwanGanz/Sonicos`. The repository must be public and contain these deployment files for either button to work.

## Requirements

- Go 1.26 or newer for local builds
- Docker for container builds
- A WhatsApp account with linked devices enabled
- Redis 7 or a compatible managed Redis service
- `ffmpeg` and `ffprobe` in `PATH` (or `FFMPEG_PATH` set) for sticker and audio conversion

## Run locally

```sh
cp .env.example .env
docker compose up -d redis
set -a && . ./.env && set +a
go run ./cmd/bot
```

On the first run, scan the QR code from WhatsApp's **Linked devices** screen. The WhatsApp session is saved to `data/sonicos.db`. Keep this file private: it contains linked-device credentials.

Build the binary with:

```sh
go vet ./...
go build -o bin/sonicos ./cmd/bot
```

## Commands

Send `!help` or `!menu` in WhatsApp to view the categorized command menu:

- **General**: `!help` (`!menu`, `!commands`), `!ping`
- **Media & Stickers**: `!sticker` (`!s`), `!stdio` (`!audiosticker`)
- **Group Management**: `!groupinfo`, `!admins`, `!tagall`, `!hidetag`, `!add`, `!kick`, `!promote`, `!demote`, `!mute`, `!unmute`, `!lock`, `!unlock`, `!setname`, `!setdesc`, `!setpp`, `!link`, `!revokelink`, `!approval`, `!requests`, `!approve`, `!reject`, `!addmode`, `!ephemeral`, `!del`, `!leave`

## Architecture

The runtime is split into explicit layers:

- `cmd/bot` wires configuration, storage, Redis, commands, and the WhatsApp adapter.
- `internal/application` handles command dispatch, timeouts, idempotency, and unknown-command responses.
- `internal/whatsapp` owns the whatsmeow connection, bounded worker pool, pairing, media/group adapters, and codec boundary.
- `internal/whatsapp/codec.go` converts WhatsApp protobuf events to command inputs and serializes quoted WhatsApp replies.
- `internal/command` provides the thread-safe command registry and handler contracts.
- `internal/plugins` contains command plugins (`help`, `ping`, `sticker`, `group`).
- `internal/sticker` handles `ffmpeg`/`ffprobe` WebP and Opus conversion.
- `internal/idempotency` implements the Redis-backed idempotency contract.

Application processing is independent of whatsmeow protobuf types. WhatsApp-specific encoding and decoding stays at the transport adapter boundary, making command processing easier to test and extend.

## Deploy to Railway

The Railway configuration builds the root [Dockerfile](Dockerfile) and restarts the worker after failures. Railway is the recommended host because its persistent volumes can preserve the SQLite WhatsApp session.

1. Push this project to a public GitHub repository and update the deploy button URL above if its owner or repository differs.
2. Select **Deploy on Railway** and connect the repository.
3. Add a Redis service, or use an existing Redis-compatible endpoint. Set the bot's `REDIS_URL` to that service's connection URL.
4. Add a persistent volume to the bot service and set its mount path to `/app/data`.
5. Deploy. Open the bot service logs and scan the QR code it prints.

The bot uses SQLite at `./data/sonicos.db`, which resolves to `/app/data/sonicos.db` in the container. Do not run multiple bot replicas against the same SQLite file or WhatsApp session; keep the worker count at one instance.
