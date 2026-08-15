package integlife

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAIWorklogUsesEditableMarkdownCache(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "integlife.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Date(2026, 7, 26, 4, 0, 0, 0, time.UTC)
	service := NewService(store, NewClient("", "", time.Second))
	service.now = func() time.Time { return now }
	result, err := service.NewAIWorklog("Cache notes locally")
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := "ai-tasks/2026/07/"
	if !strings.HasPrefix(result.Note.Path, wantPrefix) {
		t.Fatalf("note path = %q, want prefix %q", result.Note.Path, wantPrefix)
	}
	path := filepath.Join(store.NotesDir(), result.Note.Path)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "# Cache notes locally") || !strings.Contains(string(data), aiWorklogTag) {
		t.Fatalf("cached note = %q", data)
	}

	now = now.Add(time.Minute)
	if err := os.WriteFile(path, append(data, []byte("\n- Result: done\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	notes, err := service.ListNotes(false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || notes[0].SyncedAt != nil {
		t.Fatalf("listed notes = %#v", notes)
	}
	pending, err := store.PendingNotes()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || !strings.Contains(pending[0].Content, "Result: done") {
		t.Fatalf("pending notes = %#v", pending)
	}
}

func TestRemotePathMoveUsesNestedFile(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "integlife.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	base := time.Date(2026, 7, 26, 7, 0, 0, 0, time.UTC)
	service := NewService(store, NewClient("", "", time.Second))
	service.now = func() time.Time { return base }
	created, err := service.NewAIWorklog("Move this note")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkNoteSynced(created.Note.UUID, base); err != nil {
		t.Fatal(err)
	}

	oldPath := filepath.Join(store.NotesDir(), created.Note.Path)
	movedPath := "archive/2026/moved.md"
	conflict, err := store.ApplyRemoteNote(NoteRecord{
		UUID:      created.Note.UUID,
		Path:      movedPath,
		Content:   created.Note.Content,
		CreatedAt: base,
		UpdatedAt: base.Add(time.Minute),
	}, base.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if conflict {
		t.Fatal("server-only path move should not conflict")
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old file still exists: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(store.NotesDir(), movedPath))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != created.Note.Content {
		t.Fatalf("moved content = %q", data)
	}
}

func TestAIWorklogConflictLeavesRemoteFileForAgentResolution(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "integlife.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	base := time.Date(2026, 7, 26, 5, 0, 0, 0, time.UTC)
	offline := NewService(store, NewClient("", "", time.Second))
	offline.now = func() time.Time { return base }
	created, err := offline.NewAIWorklog("Resolve conflict")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkNoteSynced(created.Note.UUID, base); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateSyncCursors([]string{"notes"}, base); err != nil {
		t.Fatal(err)
	}

	localPath := filepath.Join(store.NotesDir(), created.Note.Path)
	localContent := created.Note.Content + "\n- Local: changed\n"
	if err := os.WriteFile(localPath, []byte(localContent), 0o600); err != nil {
		t.Fatal(err)
	}

	remoteContent := created.Note.Content + "\n- Remote: changed\n"
	remoteSent := false
	mergedUploaded := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload syncPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		serverTime := base.Add(10 * time.Minute).Format(time.RFC3339Nano)
		if len(payload.Notes) > 0 {
			if !strings.Contains(payload.Notes[0].Content, "Merged: yes") {
				t.Fatalf("uploaded note = %q", payload.Notes[0].Content)
			}
			mergedUploaded = true
			_, _ = w.Write([]byte(`{"server_time":"` + serverTime + `"}`))
			return
		}
		if len(payload.SyncModels) == 1 && payload.SyncModels[0] == "notes" && !remoteSent {
			remoteSent = true
			response := map[string]any{
				"server_time": serverTime,
				"notes_only_on_server": []syncNote{{
					UUID:      created.Note.UUID,
					Content:   remoteContent,
					CreatedAt: base.Format(time.RFC3339Nano),
					UpdatedAt: base.Add(2 * time.Minute).Format(time.RFC3339Nano),
				}},
			}
			_ = json.NewEncoder(w).Encode(response)
			return
		}
		_, _ = w.Write([]byte(`{"server_time":"` + serverTime + `"}`))
	}))
	defer server.Close()

	service := NewService(store, NewClient(server.URL, "token", time.Second))
	service.now = func() time.Time { return base.Add(time.Minute) }
	result, err := service.SyncPending(testContext())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Failures) != 1 || !strings.Contains(result.Failures[0].Detail, "life note resolve") {
		t.Fatalf("sync failures = %#v", result.Failures)
	}
	remotePath := strings.TrimSuffix(localPath, ".md") + ".remote.md"
	remoteData, err := os.ReadFile(remotePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(remoteData) != remoteContent {
		t.Fatalf("remote conflict file = %q", remoteData)
	}
	localData, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(localData) != localContent {
		t.Fatalf("local file was overwritten: %q", localData)
	}

	merged := localContent + "- Merged: yes\n"
	if err := os.WriteFile(localPath, []byte(merged), 0o600); err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return base.Add(3 * time.Minute) }
	if _, err := service.ResolveNote(created.Note.UUID[:8], false); err != nil {
		t.Fatal(err)
	}
	result, err = service.SyncPending(testContext())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Failures) != 0 || !mergedUploaded {
		t.Fatalf("resolved sync = %#v uploaded=%v", result, mergedUploaded)
	}
	if _, err := os.Stat(remotePath); !os.IsNotExist(err) {
		t.Fatalf("remote conflict file still exists: %v", err)
	}
}

func TestRemoteLookbackDoesNotOverwriteLocalOnlyEdit(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "integlife.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	base := time.Date(2026, 7, 26, 6, 0, 0, 0, time.UTC)
	service := NewService(store, NewClient("", "", time.Second))
	service.now = func() time.Time { return base }
	created, err := service.NewAIWorklog("Keep local edit")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkNoteSynced(created.Note.UUID, base); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.NotesDir(), created.Note.Path)
	localContent := created.Note.Content + "\n- Local only\n"
	if err := os.WriteFile(path, []byte(localContent), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.RefreshNoteFiles(base.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	conflict, err := store.ApplyRemoteNote(NoteRecord{
		UUID:      created.Note.UUID,
		Content:   created.Note.Content,
		CreatedAt: base,
		UpdatedAt: base,
	}, base.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if conflict {
		t.Fatal("unchanged server copy should not conflict")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != localContent {
		t.Fatalf("local edit was overwritten: %q", data)
	}
}
