# Sonicos WhatsApp Bot

A Go WhatsApp bot built with whatsmeow. It stores WhatsApp session credentials in SQLite (or PostgreSQL), uses Redis to suppress duplicate command processing, and loads commands as separate Go plugins.

[![Deploy on Railway](https://railway.com/button.svg)](https://railway.com/deploy?repo=github.com/SafwanGanz/Sonicos)
[![Deploy to Heroku](https://www.herokucdn.com/deploy/button.svg)](https://heroku.com/deploy?template=https%3A%2F%2Fgithub.com%2FSafwanGanz%2FSonicos)


## Requirements

- Go 1.26 or newer for local builds
- Docker for container builds
- A WhatsApp account with linked devices enabled
- Redis 7 or a compatible managed Redis service
- `ffmpeg` and `ffprobe` in `PATH` (or `FFMPEG_PATH` set) for sticker and audio conversion
- `DASHBOARD_PASSWORD` (optional) — set an alphanumeric or numeric password to enable the web onboarding & management dashboard (leave empty on a local PC/VPS to run terminal-only QR pairing)

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

## Web Dashboard & Onboarding

The bot includes an embedded web onboarding dashboard (`cmd/static/index.html`) served on `$PORT` (default `8080`) when `DASHBOARD_PASSWORD` is configured.

- **PC / VPS (Terminal Mode)**: Leave `DASHBOARD_PASSWORD` empty in `.env`. No HTTP dashboard port is opened, and WhatsApp pairing uses the terminal QR code directly.
- **Railway / Heroku (Web Onboarding)**: Set `DASHBOARD_PASSWORD` in your environment variables, open the generated app URL, sign in, and link WhatsApp using either the **QR Code** or **Phone Number Pairing Code**. Terminal QR remains active at the same time.
- If `PORT` is set by a cloud host while `DASHBOARD_PASSWORD` is still empty, the dashboard stays locked (`503`) with a setup notice and exposes `/health` (`200`) so container health checks remain green.

### Dashboard login

Set a strong alphanumeric or numeric password in `DASHBOARD_PASSWORD`:

```sh
export DASHBOARD_PASSWORD=your-secure-password
go run ./cmd/bot
```

Then open `http://localhost:8080`.

### Dashboard features

- **Overview**: Bot connection status, WhatsApp account info, uptime, message/group/user counts, moderation stats, recent activity, service health
- **Connection**: QR code scan or phone number pairing to link WhatsApp; reconnect/disconnect/unlink
- **Settings**: Bot name, command prefix, description, language, timezone, auto-start, maintenance mode
- **Commands**: List commands, change prefix, enable/disable commands, set permissions (everyone/admins/owner)
- **Groups**: List groups, enable/disable bot per group, welcome messages, moderation, admin-only commands
- **Moderation**: Enable/disable media moderation, view enrolled media references, view detections (high-confidence and possible), review/confirm or dismiss detections, warning thresholds, auto-delete, removal on final violation, dry-run mode
- **Users**: View user warning counts and moderation history, reset warnings
- **Activity Logs**: Searchable, filterable, paginated log of connection events, command activity, config changes, moderation events, API errors

### Live updates

The dashboard uses Server-Sent Events (`/events`) with a polling fallback to reflect backend state without page refreshes: WhatsApp connection status, QR changes, pairing progress, bot status, recent activity, moderation events.

### API endpoints

All API endpoints require an authenticated session cookie (`sonicos_session`). Set via `POST /auth/login` with `{ "password": "..." }`.

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/auth/login` | POST | Sign in with dashboard password |
| `/auth/logout` | POST | Sign out |
| `/auth/me` | GET | Check authentication status |
| `/status` | GET | Bot connection status and stats |
| `/events` | GET | SSE live updates |
| `/qr` | GET | WhatsApp QR code data |
| `/connection/qr/refresh` | POST | Request a fresh QR code |
| `/pair` | POST | Phone number pairing (`{country_code, phone_number}`) |
| `/connection/reconnect` | POST | Reconnect WhatsApp |
| `/connection/disconnect` | POST | Disconnect WhatsApp |
| `/connection/unlink` | POST | Unlink WhatsApp account |
| `/settings` | GET/PUT | Bot settings |
| `/commands/prefix` | POST | Change command prefix |
| `/commands` | GET | List commands |
| `/commands/{name}` | PATCH | Update command (enabled/permission) |
| `/groups` | GET | List groups |
| `/groups/{id}` | GET/PATCH | Group details and settings |
| `/moderation/settings` | GET/PUT | Moderation configuration |
| `/moderation/enrolled` | GET | List enrolled media references |
| `/moderation/enrolled/{id}` | DELETE | Remove an enrolled reference |
| `/moderation/matches` | GET | List detections (`?tier=high\|possible`) |
| `/moderation/matches/{id}/review` | POST | Confirm or dismiss a detection |
| `/moderation/history` | GET | Moderation action history |
| `/users` | GET | List users |
| `/users/{jid}/history` | GET | User moderation history |
| `/users/{jid}/warnings/reset` | POST | Reset user warnings |
| `/logs` | GET | Activity logs |

### Security

- Never expose `DASHBOARD_PASSWORD` or WhatsApp session credentials in the frontend
- Sessions use random opaque server-side tokens stored in `HttpOnly`, `SameSite=Lax` cookies (with `Secure` automatically enabled over HTTPS/reverse proxies)
- Login attempts are rate-limited per IP and all authentication, onboarding, and administrative actions emit tamper-evident SHA-256 hash-chained structured JSON audit logs
- Same-origin CORS and origin verification are enforced by default (extend with `CORS_ALLOWED_ORIGINS` only if hosting the UI separately)
- Do not store secrets in browser localStorage

## Deploy to Railway

The Railway configuration builds the root [Dockerfile](Dockerfile) and restarts the service after failures. Railway is the recommended host because its persistent volumes can preserve the SQLite WhatsApp session.

The Docker image pre-installs all runtime dependencies:
- `ffmpeg` — sticker/audio conversion and TTS waveform encoding
- `Node.js 20 LTS` + `edge-tts` CLI — `!tts` text-to-speech synthesis

1. Push this project to a public GitHub repository and update the deploy button URL above if its owner or repository differs.
2. Select **Deploy on Railway** and connect the repository.
3. Add a Redis service (Railway managed Redis or any Redis-compatible endpoint). Set the bot's `REDIS_URL` to that service's connection URL. Redis is used for command idempotency and moderation strike tracking; if Redis is unavailable, the bot falls back to in-memory idempotency and starts anyway, but moderation strikes won't persist across restarts.
4. Add a persistent volume to the bot service and set its mount path to `/app/data` so the SQLite WhatsApp session survives redeploys.
5. Set `DASHBOARD_PASSWORD` to a strong alphanumeric or numeric value.
6. Deploy, open the generated Railway domain URL, sign in with `DASHBOARD_PASSWORD`, and link WhatsApp via QR scan or phone number pairing.

The bot uses SQLite at `./data/sonicos.db`, which resolves to `/app/data/sonicos.db` in the container. Do not run multiple bot replicas against the same SQLite file or WhatsApp session; keep the worker count at one instance.
