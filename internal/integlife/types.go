package integlife

import "time"

type Record struct {
	UUID      string
	LogType   string
	Content   string
	LoggedAt  time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
	SyncedAt  *time.Time
}

type SyncFailure struct {
	UUID   string
	Detail string
}

type SyncResult struct {
	Synced   int
	Pending  int
	Failures []SyncFailure
}
