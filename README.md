# life-cli

`life-cli` is a small standalone Go CLI for recording local status logs and best-effort syncing them to a LifeOnGolang-compatible sync API.

It is intentionally independent from the main `life-on-golang` backend codebase. The sync payload is defined locally in this project, and local persistence uses SQLite.

## Features

- Save a status log locally first
- Best-effort sync to `/api/notes/sync`
- Keep working offline when the network or token is unavailable
- Retry unsynced records later with `sync`
- Compatible with the old `LIFE_*` environment variables

## Install

### Download Pre-built Binary

Visit the [Releases page](https://github.com/flyfy1/life-cli/releases) and download the binary for your platform:

- **macOS Intel (x86_64)**: `life-*-darwin-amd64`
- **macOS Apple Silicon (ARM64)**: `life-*-darwin-arm64`
- **Linux x86_64**: `life-*-linux-amd64`
- **Linux ARM64**: `life-*-linux-arm64`
- **Windows x86_64**: `life-*-windows-amd64.exe`

```bash
# Make the binary executable (macOS/Linux only)
chmod +x life-*

# Verify the download
./life --version

# Optionally move to a location in your PATH
mv life-* /usr/local/bin/life
```

### Build from Source

```bash
go build -o life ./cmd/life
```

### Build for Multiple Architectures

```bash
./build.sh v1.0.0
```

This creates binaries for all supported platforms in the `./build` directory and generates SHA256 checksums.

## Usage

```bash
./life "Refactored the sync handler"
./life ai "GPT-4 is getting interesting"
./life work "deep work session" --mood focused
./life sync
```

### Commands

- `life '<content>'` - Record a thought with default category
- `life <category> '<content>'` - Record with a specific category
- `life ... --mood <mood>` - Attach a mood to the entry
- `life login` - Authenticate and save API token
- `life logout` - Remove saved API token
- `life sync` - Push all pending entries to the server
- `life --version` - Show version

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
