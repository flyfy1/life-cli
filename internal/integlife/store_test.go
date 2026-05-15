package integlife

import (
	"path/filepath"
	"testing"
	"time"
)

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
