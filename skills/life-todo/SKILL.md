---
name: life-todo
description: Manage the user's TODO list in life-on-golang. Trigger when they want to add a task, check what's on their plate, mark something done, classify todos into lists, manage todo lists, or filter by goal/milestone/deadline.
---

# life-todo

The user keeps their TODOs in life-on-golang. Each todo can optionally hang off a goal + milestone, has an optional deadline, and supports parent/child nesting.

## When to use

- *"add a todo to ..."*, *"remind me to ..."*
- *"what's on my list"*, *"what's due today / this week"*
- *"mark X as done"*, *"complete the ... todo"*
- *"show todos for the <goal> goal"*

## Primary tool: local CLI

Use the `cli/` subrepo as the default implementation path in Codex sessions. It reads the user's local integ.life SQLite database, syncs through the production API when a token is configured, and does not require MCP tools to be exposed in the current session.

```bash
cd /Users/songyy/fast/serious/life-on-golang/cli
go run ./cmd/life sync
go run ./cmd/life list list --json
go run ./cmd/life todo list --open --json
go run ./cmd/life todo tree --open --json
```

Useful commands:

- `life list add/list/update/delete` manages todo lists.
- `life todo add/list/show/update/done/delete` manages todos.
- `life todo tree --open --json` lists todos as parent/child trees. Prefer this for reports where hierarchy matters.
- `life todo reply <uuid-prefix> "<message>"` adds a synced reply to a todo.
- `life todo replies <uuid-prefix> --json` lists replies for a todo.
- `life todo update <uuid-prefix> --list <list-uuid-or-name>` classifies or moves a todo.
- `life todo update <uuid-prefix> --clear-list` removes a todo from its list.
- `life todo add --parent <todo-uuid-or-prefix> "<content>"` creates a child todo.
- `life todo update <uuid-prefix> --parent <todo-uuid-or-prefix>` moves a todo under a parent.
- `life todo update <uuid-prefix> --clear-parent` moves a todo back to root.
- `life todo add/update --deadline <RFC3339-or-YYYY-MM-DD>` sets deadlines.
- `life todo update <uuid-prefix> --clear-deadline` clears deadlines.
- `life todo add/update --goal <uuid> --milestone <uuid>` links goal and milestone ids.
- `life todo list/tree --due today|week|overdue --json` filters by deadline.
- `life todo list/tree --goal <uuid> --milestone <uuid> --search <text> --json` filters structured todo queries.
- `life sync` pulls and pushes pending todo/list changes through `https://api.integ.life`.

When the user asks Codex to implement or fix an existing TODO, add a reply to that TODO after the work is complete. The reply should include what changed, the verification commands, and the commit hash. Use `go run ./cmd/life todo reply --source Codex --actor Codex <uuid-prefix> "<message>"`, then run `go run ./cmd/life sync` and verify with `go run ./cmd/life todo replies <uuid-prefix> --json`.

For category/list-specific queries, sync first, list categories, resolve the exact list name or UUID, then filter todos:

```bash
go run ./cmd/life sync
go run ./cmd/life list list --json
go run ./cmd/life todo list --open --list "个人项目" --json
```

When list names are duplicated, prefer UUID prefixes from `life list list --json`. After bulk updates, run `life sync` and verify no pending changes remain.

## MCP fallback

If `life-on-golang` MCP tools are available in the session, they can be used instead of the CLI:

- `todo_list` — filter by goal/milestone/deadline/completed
- `todo_add` — create a new todo
- `todo_complete` — toggle completion (`completed: false` to un-complete)

## Patterns

**Adding.** Use `go run ./cmd/life todo add --list <list-name-or-uuid> "<content>"` for list/category tasks. If the user mentions a goal or project by name and MCP tools are available, call `goals_list` first to resolve the UUID before `todo_add`.

**Listing.** Default behavior is active (non-completed, non-archived) todos. In the CLI, use `go run ./cmd/life todo list --open`; only use `--done` or MCP `include_completed: true` if the user asks for history.

**Tree reports.** When the user asks what remains to do, asks for project structure, mentions parent/child TODOs, or wants a summary with hierarchy, use `go run ./cmd/life todo tree --open --json`. The JSON includes context ancestors for matching children; mark those as context rather than open work if their `context` field is true.

**Filtering.** Prefer CLI filters instead of post-processing when possible:

```bash
go run ./cmd/life todo list --open --due today --json
go run ./cmd/life todo tree --open --list "工作" --json
go run ./cmd/life todo tree --open --parent <uuid-prefix> --json
go run ./cmd/life todo list --open --goal <goal-uuid> --milestone <milestone-uuid> --json
go run ./cmd/life todo list --open --search "Memory" --json
```

**Marking done.** When the user says "I finished X", look up by content match in `go run ./cmd/life todo list --open --json`, then run `go run ./cmd/life todo done <uuid-or-prefix>`. Confirm if multiple matches.

**Replying after implementation.** Resolve the target TODO UUID first. Add a short reply after the code is committed and pushed:

- What changed in user-facing terms.
- Verification commands and whether they passed.
- The commit hash.

Use the CLI reply command:

```bash
go run ./cmd/life todo reply --source Codex --actor Codex <uuid-prefix> "<message>"
go run ./cmd/life sync
go run ./cmd/life todo replies <uuid-prefix> --json
```

**Classifying.** For uncategorized todos, first list active todo lists, then move each todo to the closest list by content. If the task is ambiguous, inspect parent/notes before choosing. Confirm with a query for active todos whose `list_uuid` is empty.

**Parenting.** Prefer CLI parent flags for hierarchy changes. Resolve the parent by UUID/prefix first when content matches are ambiguous. The CLI inherits the parent list onto children and rejects parent cycles.

```bash
go run ./cmd/life todo add --parent <parent-prefix> "Child task"
go run ./cmd/life todo update <todo-prefix> --parent <parent-prefix>
go run ./cmd/life todo update <todo-prefix> --clear-parent
```

**Deadlines and planning metadata.** Use CLI deadline and goal/milestone flags when the user gives scheduling or project metadata. Dates may be `YYYY-MM-DD` or RFC3339.

```bash
go run ./cmd/life todo add --deadline 2026-06-07 "Submit claim"
go run ./cmd/life todo update <todo-prefix> --deadline 2026-06-07
go run ./cmd/life todo update <todo-prefix> --clear-deadline
go run ./cmd/life todo update <todo-prefix> --goal <goal-uuid> --milestone <milestone-uuid>
```

## Notes

- CLI-created todos use the CLI sync path and local SQLite first; sync failures leave pending rows for a later `life sync`.
- MCP-created todos set `todo_source` to `"mcp"` on writes — easy to distinguish from app/Wear-OS-created todos.
- Sub-tasks: use `--parent <todo-uuid-or-prefix>`; do not bypass the CLI to write `parent_uuid` directly.
- The published Codex skill path is `/Users/songyy/.codex/skills/life-todo`, symlinked to `/Users/songyy/fast/serious/life-on-golang/cli/skills/life-todo`.
