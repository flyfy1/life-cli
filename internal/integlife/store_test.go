package integlife

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func testContext() context.Context {
	return context.Background()
}

func TestStoreInsertPendingAndMarkSynced(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "integlife.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer store.Close()

	now := time.Date(2026, 5, 14, 15, 55, 0, 0, time.UTC)
	record := Record{
		UUID:      "test-uuid",
		LogType:   "ai",
		Content:   "refactored sync flow",
		LoggedAt:  now,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := store.Insert(record); err != nil {
		t.Fatalf("Insert() error = %v", err)
	}

	pending, err := store.Pending()
	if err != nil {
		t.Fatalf("Pending() error = %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("len(pending) = %d, want 1", len(pending))
	}
	if pending[0].UUID != record.UUID {
		t.Fatalf("pending[0].UUID = %s, want %s", pending[0].UUID, record.UUID)
	}
	if pending[0].SyncedAt != nil {
		t.Fatalf("pending[0].SyncedAt = %v, want nil", pending[0].SyncedAt)
	}

	syncedAt := now.Add(time.Minute)
	if err := store.MarkSynced(record.UUID, syncedAt); err != nil {
		t.Fatalf("MarkSynced() error = %v", err)
	}

	pending, err = store.Pending()
	if err != nil {
		t.Fatalf("Pending() after sync error = %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("len(pending) = %d, want 0", len(pending))
	}
}

func TestStoreMigratesOldStatusLogsSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "integlife.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE status_logs (
			uuid TEXT PRIMARY KEY,
			log_type TEXT NOT NULL,
			content TEXT NOT NULL,
			logged_at TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			synced_at TEXT
		)
	`)
	if err != nil {
		t.Fatalf("create old schema error = %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close old db error = %v", err)
	}

	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer store.Close()

	var version int
	if err := store.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatalf("read user_version error = %v", err)
	}
	if version != 4 {
		t.Fatalf("user_version = %d, want 4", version)
	}
	for _, table := range []string{"todo_lists", "todos", "todo_replies", "ai_task_runs", "ai_task_events", "active_ai_runs", "sync_cursors"} {
		var name string
		if err := store.db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name); err != nil {
			t.Fatalf("missing table %s: %v", table, err)
		}
	}
	has, err := tableHasColumnForTest(store.db, "status_logs", "last_sync_error")
	if err != nil {
		t.Fatalf("tableHasColumnForTest() error = %v", err)
	}
	if !has {
		t.Fatalf("status_logs.last_sync_error missing after migration")
	}
}

func TestTodoListAndTodoLocalWrites(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "integlife.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer store.Close()

	service := NewService(store, NewClient("", "", time.Second))
	now := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	listResult, err := service.AddTodoList(testContext(), "Work", "blue", "briefcase", 10)
	if err != nil {
		t.Fatalf("AddTodoList() error = %v", err)
	}
	if listResult.List.SyncedAt != nil {
		t.Fatalf("new list SyncedAt = %v, want nil", listResult.List.SyncedAt)
	}

	now = now.Add(time.Minute)
	todoResult, err := service.AddTodo(testContext(), "Write release notes", "ship before deploy", "Work", 20)
	if err != nil {
		t.Fatalf("AddTodo() error = %v", err)
	}
	if todoResult.Todo.ListUUID != listResult.List.UUID {
		t.Fatalf("todo list uuid = %s, want %s", todoResult.Todo.ListUUID, listResult.List.UUID)
	}

	pendingLists, err := store.PendingTodoLists()
	if err != nil {
		t.Fatalf("PendingTodoLists() error = %v", err)
	}
	pendingTodos, err := store.PendingTodos()
	if err != nil {
		t.Fatalf("PendingTodos() error = %v", err)
	}
	if len(pendingLists) != 1 || len(pendingTodos) != 1 {
		t.Fatalf("pending counts = lists %d todos %d, want 1/1", len(pendingLists), len(pendingTodos))
	}

	now = now.Add(time.Minute)
	done, err := service.CompleteTodo(testContext(), todoResult.Todo.UUID[:8], true)
	if err != nil {
		t.Fatalf("CompleteTodo() error = %v", err)
	}
	if !done.Todo.Completed || done.Todo.CompletedAt == nil {
		t.Fatalf("completed todo = %v completed_at=%v, want completed with timestamp", done.Todo.Completed, done.Todo.CompletedAt)
	}
}

func TestTodoListFollowsParentLocally(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "integlife.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer store.Close()

	service := NewService(store, NewClient("", "", time.Second))
	now := time.Date(2026, 6, 2, 11, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	oldList, err := service.AddTodoList(testContext(), "Old", "", "", 0)
	if err != nil {
		t.Fatalf("AddTodoList(old) error = %v", err)
	}
	newList, err := service.AddTodoList(testContext(), "New", "", "", 1)
	if err != nil {
		t.Fatalf("AddTodoList(new) error = %v", err)
	}

	parent := TodoRecord{
		UUID: "parent-todo", Content: "parent", ListUUID: oldList.List.UUID,
		CompletionMode: "manual", CompletionSource: "manual", AIEvaluationStatus: "not_requested",
		CreatedAt: now, UpdatedAt: now, ClientCreatedAt: now, ClientUpdatedAt: now,
	}
	child := TodoRecord{
		UUID: "child-todo", ParentUUID: parent.UUID, Content: "child", ListUUID: oldList.List.UUID,
		CompletionMode: "manual", CompletionSource: "manual", AIEvaluationStatus: "not_requested",
		CreatedAt: now, UpdatedAt: now, ClientCreatedAt: now, ClientUpdatedAt: now,
	}
	if err := store.SaveTodo(parent); err != nil {
		t.Fatalf("SaveTodo(parent) error = %v", err)
	}
	if err := store.SaveTodo(child); err != nil {
		t.Fatalf("SaveTodo(child) error = %v", err)
	}

	now = now.Add(time.Minute)
	if _, err := service.UpdateTodo(testContext(), parent.UUID, func(todo *TodoRecord) error {
		todo.ListUUID = newList.List.UUID
		return nil
	}); err != nil {
		t.Fatalf("UpdateTodo(parent) error = %v", err)
	}
	updatedChild, err := store.Todo(child.UUID)
	if err != nil {
		t.Fatalf("Todo(child) error = %v", err)
	}
	if updatedChild.ListUUID != newList.List.UUID {
		t.Fatalf("child list uuid = %s, want %s", updatedChild.ListUUID, newList.List.UUID)
	}

	now = now.Add(time.Minute)
	if _, err := service.UpdateTodo(testContext(), child.UUID, func(todo *TodoRecord) error {
		todo.ListUUID = oldList.List.UUID
		return nil
	}); err != nil {
		t.Fatalf("UpdateTodo(child) error = %v", err)
	}
	updatedChild, err = store.Todo(child.UUID)
	if err != nil {
		t.Fatalf("Todo(child after override) error = %v", err)
	}
	if updatedChild.ListUUID != newList.List.UUID {
		t.Fatalf("child override list uuid = %s, want parent list %s", updatedChild.ListUUID, newList.List.UUID)
	}
}

func TestTodoParentDeadlineAndQueryFilters(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "integlife.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer store.Close()

	service := NewService(store, NewClient("", "", time.Second))
	now := time.Date(2026, 6, 6, 9, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	listResult, err := service.AddTodoList(testContext(), "Project", "", "", 0)
	if err != nil {
		t.Fatalf("AddTodoList() error = %v", err)
	}
	parentResult, err := service.AddTodo(testContext(), "Parent", "", "Project", 0)
	if err != nil {
		t.Fatalf("AddTodo(parent) error = %v", err)
	}
	deadline := time.Date(2026, 6, 7, 0, 0, 0, 0, time.UTC)
	childResult, err := service.AddTodoWithOptions(testContext(), AddTodoOptions{
		Content:       "Child task",
		ParentRef:     parentResult.Todo.UUID[:8],
		Deadline:      &deadline,
		GoalUUID:      "goal-1",
		MilestoneUUID: "milestone-1",
		Order:         1,
	})
	if err != nil {
		t.Fatalf("AddTodoWithOptions(child) error = %v", err)
	}
	if childResult.Todo.ParentUUID != parentResult.Todo.UUID {
		t.Fatalf("child parent uuid = %s, want %s", childResult.Todo.ParentUUID, parentResult.Todo.UUID)
	}
	if childResult.Todo.ListUUID != listResult.List.UUID {
		t.Fatalf("child list uuid = %s, want inherited %s", childResult.Todo.ListUUID, listResult.List.UUID)
	}

	before := deadline.Add(time.Hour)
	todos, err := service.ListTodosWithOptions(TodoListOptions{
		CompletedFilter: ptrBool(false),
		ParentRef:       parentResult.Todo.UUID[:8],
		ParentFilter:    true,
		GoalUUID:        "goal-1",
		MilestoneUUID:   "milestone-1",
		DeadlineBefore:  &before,
		Search:          "Child",
	})
	if err != nil {
		t.Fatalf("ListTodosWithOptions() error = %v", err)
	}
	if len(todos) != 1 || todos[0].UUID != childResult.Todo.UUID {
		t.Fatalf("filtered todos = %#v, want child", todos)
	}
}

func TestTodoParentCycleIsRejected(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "integlife.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer store.Close()

	service := NewService(store, NewClient("", "", time.Second))
	now := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	parent, err := service.AddTodo(testContext(), "Parent", "", "", 0)
	if err != nil {
		t.Fatalf("AddTodo(parent) error = %v", err)
	}
	child, err := service.AddTodoWithOptions(testContext(), AddTodoOptions{
		Content:   "Child",
		ParentRef: parent.Todo.UUID,
	})
	if err != nil {
		t.Fatalf("AddTodoWithOptions(child) error = %v", err)
	}
	if _, err := service.UpdateTodo(testContext(), parent.Todo.UUID, func(todo *TodoRecord) error {
		todo.ParentUUID = child.Todo.UUID
		return nil
	}); err == nil {
		t.Fatalf("UpdateTodo(parent cycle) error = nil, want error")
	}
}

func TestTodoReplyLocalWrites(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "integlife.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer store.Close()

	service := NewService(store, NewClient("", "", time.Second))
	now := time.Date(2026, 6, 3, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	todoResult, err := service.AddTodo(testContext(), "Write reply-capable CLI", "", "", 0)
	if err != nil {
		t.Fatalf("AddTodo() error = %v", err)
	}
	now = now.Add(time.Minute)
	replyResult, err := service.AddTodoReply(testContext(), todoResult.Todo.UUID[:8], "Implemented todo reply support.", "Codex", "Codex")
	if err != nil {
		t.Fatalf("AddTodoReply() error = %v", err)
	}
	if replyResult.Reply.TodoUUID != todoResult.Todo.UUID {
		t.Fatalf("reply todo uuid = %s, want %s", replyResult.Reply.TodoUUID, todoResult.Todo.UUID)
	}
	if replyResult.Reply.SourceName != "Codex" || replyResult.Reply.ActorDisplayName != "Codex" {
		t.Fatalf("reply source/actor = %q/%q, want Codex/Codex", replyResult.Reply.SourceName, replyResult.Reply.ActorDisplayName)
	}

	_, replies, err := service.ListTodoReplies(todoResult.Todo.UUID, false)
	if err != nil {
		t.Fatalf("ListTodoReplies() error = %v", err)
	}
	if len(replies) != 1 || replies[0].Content != "Implemented todo reply support." {
		t.Fatalf("replies = %#v, want one reply", replies)
	}

	pending, err := store.PendingTodoReplies()
	if err != nil {
		t.Fatalf("PendingTodoReplies() error = %v", err)
	}
	if len(pending) != 1 || pending[0].UUID != replyResult.Reply.UUID {
		t.Fatalf("pending replies = %#v, want created reply", pending)
	}
}

func ptrBool(value bool) *bool {
	return &value
}

func TestAITaskLocalWritesStartProgressHeartbeat(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "integlife.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer store.Close()

	service := NewService(store, NewClient("", "", time.Second))
	base := time.Date(2026, 5, 30, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return base }

	started, err := service.StartAITask(testContext(), AIStartOptions{
		Project: "goal:goal-1", TodoUUID: "todo-1", Title: "Implement M2", AgentName: "codex",
	})
	if err != nil {
		t.Fatalf("StartAITask() error = %v", err)
	}
	if started.Run.Status != "running" {
		t.Fatalf("started status = %s, want running", started.Run.Status)
	}

	base = base.Add(time.Minute)
	if _, err := service.ProgressAITask(testContext(), started.Run.UUID, "storage", "wrote local records"); err != nil {
		t.Fatalf("ProgressAITask() error = %v", err)
	}
	events, err := store.PendingAITaskEvents()
	if err != nil {
		t.Fatalf("PendingAITaskEvents() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	if events[0].EventType != "progress" {
		t.Fatalf("event type = %s, want progress", events[0].EventType)
	}

	base = base.Add(time.Minute)
	if _, err := service.HeartbeatAITask(testContext(), started.Run.UUID, "still working"); err != nil {
		t.Fatalf("HeartbeatAITask() error = %v", err)
	}
	eventsAfterHeartbeat, err := store.PendingAITaskEvents()
	if err != nil {
		t.Fatalf("PendingAITaskEvents() after heartbeat error = %v", err)
	}
	if len(eventsAfterHeartbeat) != 1 {
		t.Fatalf("heartbeat created event count = %d, want 1 total", len(eventsAfterHeartbeat))
	}
	run, err := store.GetAITaskRun(started.Run.UUID)
	if err != nil {
		t.Fatalf("GetAITaskRun() error = %v", err)
	}
	if run.LatestSummary != "still working" {
		t.Fatalf("LatestSummary = %q, want heartbeat summary", run.LatestSummary)
	}
	if run.LastHeartbeatAt == nil || !run.LastHeartbeatAt.Equal(base) {
		t.Fatalf("LastHeartbeatAt = %v, want %v", run.LastHeartbeatAt, base)
	}
}

func tableHasColumnForTest(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}
