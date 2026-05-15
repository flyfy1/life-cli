package integlife

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

type Service struct {
	store  *Store
	client *Client
	now    func() time.Time
}

func NewService(store *Store, client *Client) *Service {
	return &Service{
		store:  store,
		client: client,
		now:    time.Now,
	}
}

func (s *Service) LogAndMaybeSync(ctx context.Context, logType, content string) (Record, bool, string, error) {
	now := s.now().UTC()
	record := Record{
		UUID:      newUUID(),
		LogType:   logType,
		Content:   content,
		LoggedAt:  now,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.store.Insert(record); err != nil {
		return Record{}, false, "", err
	}

	if !s.client.CanSync() {
		return record, false, "INTEGLIFE_API_TOKEN not set", nil
	}

	detail, err := s.client.SyncRecord(ctx, record)
	if err != nil {
		return record, false, err.Error(), nil
	}

	syncedAt := s.now().UTC()
	if err := s.store.MarkSynced(record.UUID, syncedAt); err != nil {
		return Record{}, false, "", err
	}
	record.SyncedAt = &syncedAt
	return record, true, detail, nil
}

func (s *Service) SyncPending(ctx context.Context) (SyncResult, error) {
	if !s.client.CanSync() {
		return SyncResult{}, fmt.Errorf("INTEGLIFE_API_TOKEN not set")
	}

	pending, err := s.store.Pending()
	if err != nil {
		return SyncResult{}, err
	}

	result := SyncResult{Pending: len(pending)}
	for _, record := range pending {
		detail, err := s.client.SyncRecord(ctx, record)
		if err != nil {
			result.Failures = append(result.Failures, SyncFailure{
				UUID:   record.UUID,
				Detail: err.Error(),
			})
			continue
		}

		if err := s.store.MarkSynced(record.UUID, s.now().UTC()); err != nil {
			return SyncResult{}, fmt.Errorf("mark synced for %s after %s: %w", record.UUID, detail, err)
		}
		result.Synced++
	}

	return result, nil
}

func newUUID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("statuslog-%d", time.Now().UnixNano())
	}

	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80

	return fmt.Sprintf(
		"%s-%s-%s-%s-%s",
		hex.EncodeToString(buf[0:4]),
		hex.EncodeToString(buf[4:6]),
		hex.EncodeToString(buf[6:8]),
		hex.EncodeToString(buf[8:10]),
		hex.EncodeToString(buf[10:16]),
	)
}
