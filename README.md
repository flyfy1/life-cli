# life-cli

`life-cli` is a small standalone Go CLI for recording local status logs and best-effort syncing them to a LifeOnGolang-compatible sync API.

It is intentionally independent from the main `life-on-golang` backend codebase. The sync payload is defined locally in this project, and local persistence uses SQLite.

## Features

- Save a status log locally first
- Best-effort sync to `/api/notes/sync`
- Keep working offline when the network or token is unavailable
- Retry unsynced records later with `sync`
- Edit `#ai-worklog` Notes as local Markdown files
- Pull and push worklog Notes without overwriting two-sided changes
- Manage todos and todo lists locally with best-effort sync
- Track AI task runs and progress events locally first

## Install

### Download Pre-built Binary

Visit the [Releases page](https://github.com/integ-life/life-cli/releases) and download the binary for your platform:

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
./life note new --title "Implement cached AI worklogs"
./life note path <note_uuid_or_prefix>
./life note sync
./life list add Work --color blue --icon briefcase
./life todo add --list Work "Write release notes"
./life todo list --open
./life todo done <todo_uuid_or_prefix>
./life sync
```

### Commands

- `life '<content>'` - Record a thought with default category
- `life <category> '<content>'` - Record with a specific category
- `life ... --mood <mood>` - Attach a mood to the entry
- `life login` - Authenticate and save API token
- `life logout` - Remove saved API token
- `life sync` - Push all pending entries to the server
- `life list add/list/update/delete` - Manage todo lists
- `life todo add/list/show/update/done/delete` - Manage todos
- `life ai start/progress/heartbeat/event/block/complete/status/resume/resolve` - Track AI task execution progress
- `life note new/list/path/dir/sync/resolve` - Manage cached AI worklog Notes
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
- Todo and list writes are local first. If a token is configured, the CLI tries to sync through `/api/notes/sync` immediately and leaves failed records pending.
- AI task events use a canonical SHA-256 payload hash. `metadata-json` must not contain JSON numbers; use strings for numeric values.
- AI worklog Markdown files live beside the database in `~/.integlife/notes/`. `life note new` creates a local pending file; edit it directly, then run `life note sync`.
- Only remote Notes containing `#ai-worklog` are pulled into this cache. Deleting a cached file soft-deletes the remote Note on the next sync.
- If both copies changed, sync keeps the local file, writes the server copy as `.remote.md`, prints the conflict, and exits non-zero. Merge the files and run `life note resolve <uuid> --local`, or discard local changes with `--remote`.
- Sync failures do not delete local data.

## API compatibility

This project targets the existing LifeOnGolang `/api/notes/sync` contract for `notes`, `status_logs`, `todo_lists`, `todos`, `ai_task_runs`, and `ai_task_events`, but it does not import any code from that repository.
