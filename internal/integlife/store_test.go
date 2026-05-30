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
	if version != 2 {
		t.Fatalf("user_version = %d, want 2", version)
	}
	for _, table := range []string{"ai_task_runs", "ai_task_events", "active_ai_runs", "sync_cursors"} {
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
