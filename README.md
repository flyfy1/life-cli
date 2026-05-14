# statuslog-go

`statuslog-go` is a small standalone Go CLI for recording local status logs and best-effort syncing them to a LifeOnGolang-compatible sync API.

It is intentionally independent from the main `life-on-golang` backend codebase. The sync payload is defined locally in this project, and local persistence uses SQLite.

## Features

- Save a status log locally first
- Best-effort sync to `/api/notes/sync`
- Keep working offline when the network or token is unavailable
- Retry unsynced records later with `sync`
- Compatible with the old `LIFE_*` environment variables

## Install

```bash
go build -o statuslog .
```

## Usage

```bash
./statuslog log ai "Refactored the sync handler"
./statuslog log mood "Feeling focused today"
./statuslog sync
```

## Environment variables

Primary variables:

- `STATUSLOG_API_URL` default: `https://api.integ.life`
- `STATUSLOG_API_TOKEN` bearer token for sync
- `STATUSLOG_DB_PATH` default: `~/.statuslog/statuslog.db`

Backward-compatible aliases:

- `LIFE_API_URL`
- `LIFE_API_TOKEN`
- `LIFE_DB_PATH`

## Behavior

- `log` writes to the local SQLite database immediately.
- If a token is configured, `log` also tries to sync the new record right away.
- `sync` sends every unsynced record in timestamp order.
- Sync failures do not delete local data.

## API compatibility

This project targets the existing LifeOnGolang `/api/notes/sync` contract for `status_logs`, but it does not import any code from that repository.
