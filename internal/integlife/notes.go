package integlife

import (
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

const aiWorklogTag = "#ai-worklog"

type NoteCommandResult struct {
	Note       NoteRecord
	Synced     bool
	SyncDetail string
}

func createNoteSchema(tx *sql.Tx) error {
	if _, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS notes (
			uuid TEXT PRIMARY KEY,
			path TEXT NOT NULL UNIQUE,
			content TEXT NOT NULL,
			deleted_at TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			synced_at TEXT,
			synced_content_hash TEXT NOT NULL DEFAULT '',
			remote_content TEXT NOT NULL DEFAULT '',
			remote_updated_at TEXT,
			remote_deleted_at TEXT,
			last_sync_error TEXT NOT NULL DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS idx_notes_pending ON notes(synced_at, updated_at)
	`); err != nil {
		return fmt.Errorf("create notes schema: %w", err)
	}
	return nil
}

func (s *Store) NotesDir() string {
	return s.notesDir
}

func (s *Store) SaveNote(note NoteRecord) error {
	_, err := s.db.Exec(`
		INSERT INTO notes (
			uuid, path, content, deleted_at, created_at, updated_at, synced_at,
			synced_content_hash, remote_content, remote_updated_at, remote_deleted_at, last_sync_error
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(uuid) DO UPDATE SET
			path = excluded.path,
			content = excluded.content,
			deleted_at = excluded.deleted_at,
			created_at = excluded.created_at,
			updated_at = excluded.updated_at,
			synced_at = excluded.synced_at,
			synced_content_hash = excluded.synced_content_hash,
			remote_content = excluded.remote_content,
			remote_updated_at = excluded.remote_updated_at,
			remote_deleted_at = excluded.remote_deleted_at,
			last_sync_error = excluded.last_sync_error
	`, note.UUID, note.Path, note.Content, formatNullTime(note.DeletedAt), formatTime(note.CreatedAt),
		formatTime(note.UpdatedAt), formatNullTime(note.SyncedAt), note.SyncedContentHash,
		note.RemoteContent, formatNullTime(note.RemoteUpdatedAt), formatNullTime(note.RemoteDeletedAt),
		note.LastSyncError)
	if err != nil {
		return fmt.Errorf("save note: %w", err)
	}
	return nil
}

func (s *Store) Note(ref string) (NoteRecord, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return NoteRecord{}, fmt.Errorf("note reference must be non-empty")
	}
	rows, err := s.db.Query(noteSelectSQL()+`
		WHERE uuid = ? OR uuid LIKE ? OR path = ? OR path = ?
		ORDER BY updated_at DESC
		LIMIT 2
	`, ref, ref+"%", ref, filepath.Base(ref))
	if err != nil {
		return NoteRecord{}, fmt.Errorf("query note: %w", err)
	}
	defer rows.Close()
	var matches []NoteRecord
	for rows.Next() {
		note, err := scanNote(rows)
		if err != nil {
			return NoteRecord{}, err
		}
		matches = append(matches, note)
	}
	if len(matches) == 0 {
		return NoteRecord{}, fmt.Errorf("note not found: %s", ref)
	}
	if len(matches) > 1 {
		return NoteRecord{}, fmt.Errorf("note reference is ambiguous: %s", ref)
	}
	return matches[0], nil
}

func (s *Store) ListNotes(includeDeleted, conflictsOnly bool) ([]NoteRecord, error) {
	query := noteSelectSQL() + ` WHERE 1 = 1`
	if !includeDeleted {
		query += ` AND deleted_at IS NULL`
	}
	if conflictsOnly {
		query += ` AND last_sync_error LIKE 'conflict:%'`
	}
	query += ` ORDER BY updated_at DESC`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("query notes: %w", err)
	}
	defer rows.Close()
	var notes []NoteRecord
	for rows.Next() {
		note, err := scanNote(rows)
		if err != nil {
			return nil, err
		}
		notes = append(notes, note)
	}
	return notes, rows.Err()
}

func (s *Store) PendingNotes() ([]NoteRecord, error) {
	rows, err := s.db.Query(noteSelectSQL() + `
		WHERE synced_at IS NULL AND last_sync_error NOT LIKE 'conflict:%'
		ORDER BY updated_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query pending notes: %w", err)
	}
	defer rows.Close()
	var notes []NoteRecord
	for rows.Next() {
		note, err := scanNote(rows)
		if err != nil {
			return nil, err
		}
		notes = append(notes, note)
	}
	return notes, rows.Err()
}

func (s *Store) PendingNoteCount() (int, error) {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM notes WHERE synced_at IS NULL`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count pending notes: %w", err)
	}
	return count, nil
}

func (s *Store) MarkNoteSynced(uuid string, syncedAt time.Time) error {
	var content string
	var deletedAtRaw sql.NullString
	if err := s.db.QueryRow(`SELECT content, deleted_at FROM notes WHERE uuid = ?`, uuid).Scan(&content, &deletedAtRaw); err != nil {
		return fmt.Errorf("read synced note: %w", err)
	}
	deletedAt, err := parseNullTime(deletedAtRaw)
	if err != nil {
		return fmt.Errorf("parse synced note deletion: %w", err)
	}
	_, err = s.db.Exec(`
		UPDATE notes
		SET synced_at = ?, synced_content_hash = ?, remote_content = '',
			remote_updated_at = NULL, remote_deleted_at = NULL, last_sync_error = ''
		WHERE uuid = ?
	`, formatTime(syncedAt), noteStateHash(content, deletedAt), uuid)
	if err != nil {
		return fmt.Errorf("mark note synced: %w", err)
	}
	return nil
}

func (s *Store) MarkNoteSyncError(uuid, detail string) error {
	if _, err := s.db.Exec(`UPDATE notes SET last_sync_error = ? WHERE uuid = ?`, detail, uuid); err != nil {
		return fmt.Errorf("mark note sync error: %w", err)
	}
	return nil
}

func (s *Store) RefreshNoteFiles(now time.Time) error {
	notes, err := s.ListNotes(true, false)
	if err != nil {
		return err
	}
	for _, note := range notes {
		path, err := s.noteFilePath(note.Path)
		if err != nil {
			return err
		}
		data, readErr := os.ReadFile(path)
		if errors.Is(readErr, os.ErrNotExist) {
			if note.DeletedAt == nil && !strings.HasPrefix(note.LastSyncError, "conflict:") {
				deletedAt := now.UTC()
				note.DeletedAt = &deletedAt
				note.UpdatedAt = deletedAt
				note.SyncedAt = nil
				note.LastSyncError = ""
				if err := s.SaveNote(note); err != nil {
					return err
				}
			}
			continue
		}
		if readErr != nil {
			return fmt.Errorf("read note %s: %w", path, readErr)
		}
		content := string(data)
		if note.DeletedAt == nil && content == note.Content {
			continue
		}
		note.Content = content
		note.DeletedAt = nil
		note.UpdatedAt = now.UTC()
		note.SyncedAt = nil
		if !strings.HasPrefix(note.LastSyncError, "conflict:") {
			note.LastSyncError = ""
		}
		if err := s.SaveNote(note); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ApplyRemoteNote(remote NoteRecord, syncedAt time.Time) (bool, error) {
	local, err := s.noteByUUID(remote.UUID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if errors.Is(err, sql.ErrNoRows) {
		if remote.DeletedAt != nil || !isAIWorklog(remote.Content) {
			return false, nil
		}
		if remote.Path == "" {
			remote.Path = noteFilename(remote)
		}
		remote.SyncedAt = &syncedAt
		remote.SyncedContentHash = noteStateHash(remote.Content, remote.DeletedAt)
		if err := s.writeNoteFile(remote.Path, remote.Content); err != nil {
			return false, err
		}
		return false, s.SaveNote(remote)
	}

	localChanged := noteStateHash(local.Content, local.DeletedAt) != local.SyncedContentHash
	remoteChanged := noteStateHash(remote.Content, remote.DeletedAt) != local.SyncedContentHash
	remotePathChanged := remote.Path != "" && remote.Path != local.Path
	if local.SyncedContentHash == "" {
		remoteChanged = noteStateHash(remote.Content, remote.DeletedAt) != noteStateHash(local.Content, local.DeletedAt)
	}
	if localChanged {
		if remoteChanged || remotePathChanged {
			return true, s.ApplyNoteConflict(local.UUID, remote)
		}
		return false, nil
	}

	if remote.Path == "" {
		remote.Path = local.Path
	}
	remote.SyncedAt = &syncedAt
	remote.SyncedContentHash = noteStateHash(remote.Content, remote.DeletedAt)
	if remote.DeletedAt != nil {
		if err := os.Remove(s.mustNoteFilePath(local.Path)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("remove deleted note: %w", err)
		}
	} else if err := s.writeNoteFile(remote.Path, remote.Content); err != nil {
		return false, err
	}
	if remote.Path != local.Path {
		_ = os.Remove(s.mustNoteFilePath(local.Path))
	}
	_ = os.Remove(s.remoteNotePath(local.Path))
	return false, s.SaveNote(remote)
}

func (s *Store) ApplyNoteConflict(uuid string, remote NoteRecord) error {
	local, err := s.noteByUUID(uuid)
	if err != nil {
		return err
	}
	local.RemoteContent = remote.Content
	local.RemoteUpdatedAt = &remote.UpdatedAt
	local.RemoteDeletedAt = remote.DeletedAt
	local.SyncedAt = nil
	local.LastSyncError = "conflict: local and server changed; merge the local and .remote.md files, then run life note resolve " + shortUUID(uuid) + " --local"
	remoteBody := remote.Content
	if remote.DeletedAt != nil {
		remoteBody = "<!-- The server deleted this note. -->\n"
	}
	if err := writeFileAtomic(s.remoteNotePath(local.Path), []byte(remoteBody)); err != nil {
		return err
	}
	return s.SaveNote(local)
}

func (s *Store) ResolveNote(ref string, preferRemote bool, now time.Time) (NoteRecord, error) {
	note, err := s.Note(ref)
	if err != nil {
		return NoteRecord{}, err
	}
	if !strings.HasPrefix(note.LastSyncError, "conflict:") {
		return NoteRecord{}, fmt.Errorf("note has no unresolved conflict: %s", ref)
	}
	if preferRemote {
		note.Content = note.RemoteContent
		note.UpdatedAt = valueOrTime(note.RemoteUpdatedAt, now.UTC())
		note.DeletedAt = note.RemoteDeletedAt
		note.SyncedContentHash = noteStateHash(note.Content, note.DeletedAt)
		syncedAt := now.UTC()
		note.SyncedAt = &syncedAt
		if note.DeletedAt != nil {
			if err := os.Remove(s.mustNoteFilePath(note.Path)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return NoteRecord{}, err
			}
		} else if err := s.writeNoteFile(note.Path, note.Content); err != nil {
			return NoteRecord{}, err
		}
	} else {
		path := s.mustNoteFilePath(note.Path)
		data, err := os.ReadFile(path)
		if err != nil {
			return NoteRecord{}, fmt.Errorf("read merged local note: %w", err)
		}
		note.Content = string(data)
		note.DeletedAt = nil
		note.SyncedContentHash = noteStateHash(note.RemoteContent, note.RemoteDeletedAt)
		note.UpdatedAt = afterRemote(now.UTC(), note.RemoteUpdatedAt)
		note.SyncedAt = nil
	}
	note.RemoteContent = ""
	note.RemoteUpdatedAt = nil
	note.RemoteDeletedAt = nil
	note.LastSyncError = ""
	_ = os.Remove(s.remoteNotePath(note.Path))
	if err := s.SaveNote(note); err != nil {
		return NoteRecord{}, err
	}
	return note, nil
}

func (s *Service) NewAIWorklog(title string) (NoteCommandResult, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return NoteCommandResult{}, fmt.Errorf("title must be non-empty")
	}
	now := s.now().UTC()
	note := NoteRecord{
		UUID:      newUUID(),
		Content:   "# " + title + "\n\n" + aiWorklogTag + "\n",
		CreatedAt: now,
		UpdatedAt: now,
	}
	note.Path = noteFilename(note)
	if err := s.store.writeNoteFile(note.Path, note.Content); err != nil {
		return NoteCommandResult{}, err
	}
	if err := s.store.SaveNote(note); err != nil {
		return NoteCommandResult{}, err
	}
	updated, err := s.store.Note(note.UUID)
	if err != nil {
		return NoteCommandResult{}, err
	}
	return NoteCommandResult{Note: updated, SyncDetail: "edit the Markdown file, then run life note sync"}, nil
}

func (s *Service) ListNotes(includeDeleted, conflictsOnly bool) ([]NoteRecord, error) {
	if err := s.store.RefreshNoteFiles(s.now()); err != nil {
		return nil, err
	}
	return s.store.ListNotes(includeDeleted, conflictsOnly)
}

func (s *Service) Note(ref string) (NoteRecord, error) {
	return s.store.Note(ref)
}

func (s *Service) NotesDir() string {
	return s.store.NotesDir()
}

func (s *Service) ResolveNote(ref string, preferRemote bool) (NoteRecord, error) {
	return s.store.ResolveNote(ref, preferRemote, s.now())
}

func (s *Store) noteByUUID(uuid string) (NoteRecord, error) {
	return scanNote(s.db.QueryRow(noteSelectSQL()+` WHERE uuid = ?`, uuid))
}

func (s *Store) noteFilePath(name string) (string, error) {
	name = strings.TrimSpace(name)
	rel := filepath.Clean(filepath.FromSlash(name))
	if name == "" || strings.Contains(name, "\\") || rel == "." || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid cached note path: %q", name)
	}
	if !strings.EqualFold(filepath.Ext(rel), ".md") {
		return "", fmt.Errorf("cached note path must end in .md: %q", name)
	}
	return filepath.Join(s.notesDir, rel), nil
}

func (s *Store) mustNoteFilePath(name string) string {
	path, _ := s.noteFilePath(name)
	return path
}

func (s *Store) remoteNotePath(name string) string {
	path := s.mustNoteFilePath(name)
	return strings.TrimSuffix(path, filepath.Ext(path)) + ".remote.md"
}

func (s *Store) writeNoteFile(name, content string) error {
	path, err := s.noteFilePath(name)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, []byte(content))
}

func writeFileAtomic(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create notes directory: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".note-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary note: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(content); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace cached note: %w", err)
	}
	return nil
}

func noteFilename(note NoteRecord) string {
	title := "ai-worklog"
	for _, line := range strings.Split(note.Content, "\n") {
		if value := strings.TrimSpace(strings.TrimPrefix(line, "# ")); strings.HasPrefix(line, "# ") && value != "" {
			title = value
			break
		}
	}
	stamp := note.CreatedAt.UTC().Format("20060102-1504")
	return filepath.ToSlash(filepath.Join("inbox", fmt.Sprintf("%s-%s--%s.md", stamp, filenameSlug(title), filenameSlug(note.UUID))))
}

func filenameSlug(value string) string {
	var out []rune
	dash := false
	for _, r := range strings.ToLower(value) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			out = append(out, r)
			dash = false
			continue
		}
		if !dash && len(out) > 0 {
			out = append(out, '-')
			dash = true
		}
	}
	slug := strings.Trim(string(out), "-")
	if slug == "" {
		return "ai-worklog"
	}
	return slug
}

func isAIWorklog(content string) bool {
	return strings.Contains(strings.ToLower(content), aiWorklogTag)
}

func noteStateHash(content string, deletedAt *time.Time) string {
	state := "active\x00"
	if deletedAt != nil {
		state = "deleted\x00"
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(state+content)))
}

func afterRemote(now time.Time, remote *time.Time) time.Time {
	if remote != nil && !now.After(*remote) {
		return remote.Add(time.Nanosecond)
	}
	return now
}

func valueOrTime(value *time.Time, fallback time.Time) time.Time {
	if value != nil {
		return value.UTC()
	}
	return fallback
}

func shortUUID(value string) string {
	if len(value) <= 8 {
		return value
	}
	return value[:8]
}

func noteSelectSQL() string {
	return `SELECT uuid, path, content, deleted_at, created_at, updated_at, synced_at,
		synced_content_hash, remote_content, remote_updated_at, remote_deleted_at, last_sync_error
		FROM notes`
}

func scanNote(scanner interface{ Scan(dest ...any) error }) (NoteRecord, error) {
	var note NoteRecord
	var deletedAt, syncedAt, remoteUpdatedAt, remoteDeletedAt sql.NullString
	var createdAt, updatedAt string
	if err := scanner.Scan(
		&note.UUID, &note.Path, &note.Content, &deletedAt, &createdAt, &updatedAt, &syncedAt,
		&note.SyncedContentHash, &note.RemoteContent, &remoteUpdatedAt, &remoteDeletedAt,
		&note.LastSyncError,
	); err != nil {
		return NoteRecord{}, err
	}
	var err error
	if note.DeletedAt, err = parseNullTime(deletedAt); err != nil {
		return NoteRecord{}, err
	}
	if note.SyncedAt, err = parseNullTime(syncedAt); err != nil {
		return NoteRecord{}, err
	}
	if note.RemoteUpdatedAt, err = parseNullTime(remoteUpdatedAt); err != nil {
		return NoteRecord{}, err
	}
	if note.RemoteDeletedAt, err = parseNullTime(remoteDeletedAt); err != nil {
		return NoteRecord{}, err
	}
	if note.CreatedAt, err = parseTime(createdAt); err != nil {
		return NoteRecord{}, err
	}
	if note.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return NoteRecord{}, err
	}
	return note, nil
}
