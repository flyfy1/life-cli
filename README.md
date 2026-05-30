# life-cli

`life-cli` is a small standalone Go CLI for recording local status logs and best-effort syncing them to a LifeOnGolang-compatible sync API.

It is intentionally independent from the main `life-on-golang` backend codebase. The sync payload is defined locally in this project, and local persistence uses SQLite.

## Features

- Save a status log locally first
- Best-effort sync to `/api/notes/sync`
- Keep working offline when the network or token is unavailable
- Retry unsynced records later with `sync`
- Track AI task runs and progress events locally first

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
./life ai start --project goal:<uuid> --todo <uuid> --title "Implement sync" --agent codex --json
./life ai progress --run <run_uuid> --phase coding --summary "Wrote local storage"
./life ai complete --run <run_uuid> --summary "Finished implementation"
./life sync
```

### Commands

- `life '<content>'` - Record a thought with default category
- `life <category> '<content>'` - Record with a specific category
- `life ... --mood <mood>` - Attach a mood to the entry
- `life login` - Authenticate and save API token
- `life logout` - Remove saved API token
- `life sync` - Push all pending entries to the server
- `life ai start/progress/heartbeat/event/block/complete/status/resume/resolve` - Track AI task execution progress
- `life --version` - Show version

## Environment variables

Primary variables:

- `INTEGLIFE_API_URL` default: `https://api.integ.life`
- `INTEGLIFE_API_TOKEN` bearer token for sync, stored by `life login`
- `INTEGLIFE_DB_PATH` default: `~/.integlife/integlife.db`
- `LIFE_AI_SESSION_ID` optional AI run session id used by `life ai resume`

## Behavior

- `log` writes to the local SQLite database immediately.
- If a token is configured, `log` also tries to sync the new record right away.
- `sync` sends every unsynced record in timestamp order.
- AI task events use a canonical SHA-256 payload hash. `metadata-json` must not contain JSON numbers; use strings for numeric values.
- Sync failures do not delete local data.

## API compatibility

This project targets the existing LifeOnGolang `/api/notes/sync` contract for `status_logs`, but it does not import any code from that repository.
