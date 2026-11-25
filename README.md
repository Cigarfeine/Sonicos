# Sonicos WhatsApp Bot

A Go WhatsApp bot built with whatsmeow. It stores WhatsApp session credentials in SQLite, uses Redis to suppress duplicate command processing, and loads commands as separate Go plugins.

[![Deploy on Railway](https://railway.com/button.svg)](https://railway.com/deploy?repo=github.com/SafwanGanz/Sonicos)
[![Deploy to Heroku](https://www.herokucdn.com/deploy/button.svg)](https://heroku.com/deploy?template=https%3A%2F%2Fgithub.com%2FSafwanGanz%2FSonicos)

> The deploy buttons target `https://github.com/SafwanGanz/Sonicos`. The repository must be public and contain these deployment files for either button to work. That URL currently returns 404, so publish the repository or update the button URLs and `app.json` repository field before deploying.

## Requirements

- Go 1.26 or newer for local builds
- Docker for container builds
- A WhatsApp account with linked devices enabled
- Redis 7 or a compatible managed Redis service

## Run locally

```sh
cp .env.example .env
docker compose up -d redis
set -a && . ./.env && set +a
go run ./cmd/bot
```

On the first run, scan the QR code from WhatsApp's **Linked devices** screen. The WhatsApp session is saved to `data/sonicos.db`. Keep this file private: it contains linked-device credentials.

Run checks and build the binary with:

```sh
go test ./...
go build -o bin/sonicos ./cmd/bot
```

## Architecture

The runtime is split into explicit layers:

- `cmd/bot` wires configuration, storage, Redis, commands, and the WhatsApp adapter.
- `internal/application` handles command dispatch, timeouts, idempotency, and unknown-command responses.
- `internal/whatsapp` owns the whatsmeow connection, bounded worker pool, pairing, and adapter boundary.
- `internal/whatsapp/codec.go` converts WhatsApp protobuf events to command inputs and serializes quoted WhatsApp replies.
- `internal/command` provides the thread-safe command registry and handler contracts.
- `internal/plugins` contains one file per command plugin.
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

## Deploy to Heroku

The [Heroku container manifest](heroku.yml) runs one worker process. The [app manifest](app.json) requests a worker dyno and prompts for `REDIS_URL`.

1. Push this project to a public GitHub repository and update the Heroku button URL and `app.json` repository field if needed.
2. Select **Deploy to Heroku**, provide a Redis connection URL, and create the app.
3. Open the app's worker logs and scan the QR code.

**Heroku does not provide persistent local disk for dynos.** The SQLite database can be deleted when a dyno restarts or is redeployed, which means the bot may require WhatsApp pairing again. The Heroku button is provided for testing; use Railway with a volume for a persistent SQLite session. Heroku also requires an active paid worker dyno for a continuously running bot.

## Configuration

| Variable | Default | Description |
| --- | --- | --- |
| `DATABASE_DRIVER` | `sqlite3` | whatsmeow session database driver; `sqlite3` or `pgx` |
| `DATABASE_URL` | `file:./data/sonicos.db?...` | SQLite file URL or PostgreSQL connection URL |
| `REDIS_URL` | `redis://localhost:6379/0` | Redis connection URL; required at startup |
| `BOT_PREFIX` | `!` | Command prefix |
| `WORKERS` | `8` | Concurrent command workers |
| `QUEUE_SIZE` | `512` | Maximum buffered incoming messages |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error` |

Prisma is not used by this Go application; it is a JavaScript/TypeScript ORM. whatsmeow manages its own SQLite session schema. Do not run Prisma migrations against the whatsmeow session database.

## Add a command

Create a separate Go file in `internal/plugins`, then register its handler with `command.Register` from the plugin's `init` function. The application imports the plugin package, so each command remains isolated from the command registry. Existing commands are `!help`, `!commands`, and `!ping`.

## Runtime notes

Incoming WhatsApp events enter a bounded queue and are handled by a configurable worker pool. Redis stores idempotency keys; SQLite stores the WhatsApp session. Run only one bot process for each linked-device session. Back up the SQLite database securely and never commit `.env` or session files.
