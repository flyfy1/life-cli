package integlife

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestBuildSyncPayload(t *testing.T) {
	now := time.Date(2026, 5, 14, 16, 0, 0, 123000000, time.UTC)
	record := Record{
		UUID:      "uuid-1",
		LogType:   "mood",
		Content:   "focused",
		LoggedAt:  now,
		CreatedAt: now,
		UpdatedAt: now,
	}

	payload := buildSyncPayload(record)
	if len(payload.SyncModels) != 1 || payload.SyncModels[0] != "status_logs" {
		t.Fatalf("payload.SyncModels = %v, want [status_logs]", payload.SyncModels)
	}
	if len(payload.StatusLogs) != 1 {
		t.Fatalf("len(payload.StatusLogs) = %d, want 1", len(payload.StatusLogs))
	}
	if payload.StatusLogs[0].UUID != "uuid-1" {
		t.Fatalf("UUID = %s, want uuid-1", payload.StatusLogs[0].UUID)
	}
	if payload.StatusLogs[0].LogType != "mood" {
		t.Fatalf("LogType = %s, want mood", payload.StatusLogs[0].LogType)
	}
	if payload.StatusLogs[0].Content != "focused" {
		t.Fatalf("Content = %s, want focused", payload.StatusLogs[0].Content)
	}
	if payload.StatusLogs[0].LoggedAt != now.Format(time.RFC3339Nano) {
		t.Fatalf("LoggedAt = %s, want %s", payload.StatusLogs[0].LoggedAt, now.Format(time.RFC3339Nano))
	}
}

func TestBuildSyncBatchPayloadIncludesAIModelsAndCursors(t *testing.T) {
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	cursor := now.Add(-time.Hour)
	payload := buildSyncBatchPayload(SyncBatch{
		StatusLogs: []Record{{UUID: "status-1", LogType: "ai", Content: "hello", LoggedAt: now, CreatedAt: now, UpdatedAt: now}},
		AITaskRuns: []AITaskRunRecord{{
			UUID: "run-1", ProjectType: "goal", ProjectUUID: "goal-1", TodoUUID: "todo-1",
			AgentName: "codex", Status: "running", CreatedAt: now, UpdatedAt: now, ClientCreatedAt: now, ClientUpdatedAt: now,
		}},
		AITaskEvents: []AITaskEventRecord{{
			UUID: "event-1", RunUUID: "run-1", TodoUUID: "todo-1", EventType: "progress", Severity: "info",
			MetadataJSON: "{}", PayloadHashVersion: 1, PayloadHash: "hash", OccurredAt: now, CreatedAt: now, UpdatedAt: now,
			ClientCreatedAt: now, ClientUpdatedAt: now,
		}},
		Cursors: map[string]time.Time{"ai_task_runs": cursor, "ai_task_events": cursor},
	})

	wantModels := []string{"status_logs", "ai_task_runs", "ai_task_events"}
	if !reflect.DeepEqual(payload.SyncModels, wantModels) {
		t.Fatalf("SyncModels = %v, want %v", payload.SyncModels, wantModels)
	}
	if payload.LastSyncAtByModel["status_logs"] != nil {
		t.Fatalf("status_logs cursor = %v, want nil", payload.LastSyncAtByModel["status_logs"])
	}
	for _, model := range []string{"ai_task_runs", "ai_task_events"} {
		got := payload.LastSyncAtByModel[model]
		if got == nil || *got != cursor.Format(time.RFC3339Nano) {
			t.Fatalf("%s cursor = %v, want %s", model, got, cursor.Format(time.RFC3339Nano))
		}
	}
	if len(payload.AITaskRuns) != 1 || len(payload.AITaskEvents) != 1 {
		t.Fatalf("AI payload counts = runs %d events %d, want 1/1", len(payload.AITaskRuns), len(payload.AITaskEvents))
	}
}

func TestSyncResponseAckHandling(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "integlife.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer store.Close()

	now := time.Date(2026, 5, 30, 13, 0, 0, 0, time.UTC)
	runOK := testRun("run-ok", now)
	runConflict := testRun("run-conflict", now)
	if err := store.SaveAITaskRun(runOK); err != nil {
		t.Fatalf("SaveAITaskRun(runOK) error = %v", err)
	}
	if err := store.SaveAITaskRun(runConflict); err != nil {
		t.Fatalf("SaveAITaskRun(runConflict) error = %v", err)
	}
	for _, event := range []AITaskEventRecord{
		testEvent(t, "event-accepted", "run-ok", now),
		testEvent(t, "event-duplicate", "run-ok", now.Add(time.Second)),
		testEvent(t, "event-rejected", "run-ok", now.Add(2*time.Second)),
	} {
		if err := store.SaveAITaskEvent(event); err != nil {
			t.Fatalf("SaveAITaskEvent(%s) error = %v", event.UUID, err)
		}
	}

	serverTime := now.Add(time.Minute).Format(time.RFC3339Nano)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/notes/sync" {
			t.Fatalf("path = %s, want /api/notes/sync", r.URL.Path)
		}
		var payload syncPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request payload error = %v", err)
		}
		if len(payload.AITaskRuns) != 2 || len(payload.AITaskEvents) != 3 {
			t.Fatalf("payload counts = runs %d events %d, want 2/3", len(payload.AITaskRuns), len(payload.AITaskEvents))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"server_time":"` + serverTime + `",
			"ai_task_runs_server_newer":[{"uuid":"run-conflict"}],
			"ai_task_events_accepted":["event-accepted"],
			"ai_task_events_duplicate":["event-duplicate"],
			"ai_task_events_rejected":[{"uuid":"event-rejected","reason":"hash_mismatch"}]
		}`))
	}))
	defer server.Close()

	service := NewService(store, NewClient(server.URL, "token", time.Second))
	result, err := service.SyncPending(testContext())
	if err != nil {
		t.Fatalf("SyncPending() error = %v", err)
	}
	if result.Synced != 3 {
		t.Fatalf("result.Synced = %d, want 3", result.Synced)
	}

	syncedRun, err := store.GetAITaskRun("run-ok")
	if err != nil {
		t.Fatalf("GetAITaskRun(run-ok) error = %v", err)
	}
	if syncedRun.SyncedAt == nil {
		t.Fatalf("run-ok SyncedAt = nil, want set")
	}
	conflictRun, err := store.GetAITaskRun("run-conflict")
	if err != nil {
		t.Fatalf("GetAITaskRun(run-conflict) error = %v", err)
	}
	if conflictRun.SyncedAt != nil || conflictRun.LastSyncError != "conflict: server newer" {
		t.Fatalf("conflict run synced/error = %v/%q", conflictRun.SyncedAt, conflictRun.LastSyncError)
	}

	pendingEvents, err := store.PendingAITaskEvents()
	if err != nil {
		t.Fatalf("PendingAITaskEvents() error = %v", err)
	}
	if len(pendingEvents) != 1 || pendingEvents[0].UUID != "event-rejected" || pendingEvents[0].LastSyncError != "hash_mismatch" {
		t.Fatalf("pending events = %#v, want rejected hash_mismatch only", pendingEvents)
	}
}

func testRun(uuid string, now time.Time) AITaskRunRecord {
	return AITaskRunRecord{
		UUID: uuid, ProjectType: "goal", ProjectUUID: "goal-1", TodoUUID: "todo-1",
		AgentName: "codex", Status: "running", ContextSnapshotJSON: "{}",
		CreatedAt: now, UpdatedAt: now, ClientCreatedAt: now, ClientUpdatedAt: now,
	}
}

func testEvent(t *testing.T, uuid, runUUID string, now time.Time) AITaskEventRecord {
	t.Helper()
	event := AITaskEventRecord{
		UUID: uuid, RunUUID: runUUID, TodoUUID: "todo-1", EventType: "progress", Severity: "info",
		Title: "Progress", Content: uuid, MetadataJSON: "{}", PayloadHashVersion: 1,
		OccurredAt: now, CreatedAt: now, UpdatedAt: now, ClientCreatedAt: now, ClientUpdatedAt: now,
	}
	hash, metadata, err := ComputeAITaskEventPayloadHash(event)
	if err != nil {
		t.Fatalf("ComputeAITaskEventPayloadHash() error = %v", err)
	}
	event.PayloadHash = hash
	event.MetadataJSON = metadata
	return event
}
