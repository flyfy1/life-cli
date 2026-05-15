package integlife

import (
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
