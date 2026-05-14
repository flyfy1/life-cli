package statuslog

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func OpenStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite db: %w", err)
	}

	store := &Store{db: db}
	if err := store.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) init() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS status_logs (
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
		return fmt.Errorf("init schema: %w", err)
	}
	return nil
}

func (s *Store) Insert(record Record) error {
	_, err := s.db.Exec(
		`INSERT INTO status_logs (uuid, log_type, content, logged_at, created_at, updated_at, synced_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		record.UUID,
		record.LogType,
		record.Content,
		record.LoggedAt.UTC().Format(time.RFC3339Nano),
		record.CreatedAt.UTC().Format(time.RFC3339Nano),
		record.UpdatedAt.UTC().Format(time.RFC3339Nano),
		formatNullTime(record.SyncedAt),
	)
	if err != nil {
		return fmt.Errorf("insert record: %w", err)
	}
	return nil
}

func (s *Store) MarkSynced(uuid string, syncedAt time.Time) error {
	_, err := s.db.Exec(
		`UPDATE status_logs SET synced_at = ? WHERE uuid = ?`,
		syncedAt.UTC().Format(time.RFC3339Nano),
		uuid,
	)
	if err != nil {
		return fmt.Errorf("mark synced: %w", err)
	}
	return nil
}

func (s *Store) Pending() ([]Record, error) {
	rows, err := s.db.Query(`
		SELECT uuid, log_type, content, logged_at, created_at, updated_at, synced_at
		FROM status_logs
		WHERE synced_at IS NULL
		ORDER BY logged_at ASC, created_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query pending records: %w", err)
	}
	defer rows.Close()

	var records []Record
	for rows.Next() {
		record, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending records: %w", err)
	}
	return records, nil
}

func scanRecord(scanner interface {
	Scan(dest ...any) error
}) (Record, error) {
	var record Record
	var loggedAt string
	var createdAt string
	var updatedAt string
	var syncedAt sql.NullString

	if err := scanner.Scan(
		&record.UUID,
		&record.LogType,
		&record.Content,
		&loggedAt,
		&createdAt,
		&updatedAt,
		&syncedAt,
	); err != nil {
		return Record{}, fmt.Errorf("scan record: %w", err)
	}

	var err error
	record.LoggedAt, err = time.Parse(time.RFC3339Nano, loggedAt)
	if err != nil {
		return Record{}, fmt.Errorf("parse logged_at: %w", err)
	}
	record.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return Record{}, fmt.Errorf("parse created_at: %w", err)
	}
	record.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return Record{}, fmt.Errorf("parse updated_at: %w", err)
	}

	if syncedAt.Valid {
		parsed, err := time.Parse(time.RFC3339Nano, syncedAt.String)
		if err != nil {
			return Record{}, fmt.Errorf("parse synced_at: %w", err)
		}
		record.SyncedAt = &parsed
	}

	return record, nil
}

func formatNullTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}
