package integlife

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	apiURL     string
	apiToken   string
	httpClient *http.Client
}

func NewClient(apiURL, apiToken string, timeout time.Duration) *Client {
	return &Client{
		apiURL:   strings.TrimRight(apiURL, "/"),
		apiToken: apiToken,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) CanSync() bool {
	return c.apiToken != ""
}

// Login authenticates with username/password, creates a named API token, and returns the secret.
func (c *Client) Login(ctx context.Context, username, password string) (string, error) {
	// Step 1: get JWT via username/password
	loginBody, _ := json.Marshal(map[string]string{"username": username, "password": password})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL+"/login", bytes.NewReader(loginBody))
	if err != nil {
		return "", fmt.Errorf("build login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("send login request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return "", fmt.Errorf("login http %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	var loginResp struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&loginResp); err != nil {
		return "", fmt.Errorf("decode login response: %w", err)
	}
	if loginResp.Token == "" {
		return "", fmt.Errorf("empty token in login response")
	}

	// Step 2: create a persistent API token using the JWT
	tokenBody, _ := json.Marshal(map[string]string{"name": "life-cli"})
	req2, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL+"/api/tokens", bytes.NewReader(tokenBody))
	if err != nil {
		return "", fmt.Errorf("build create-token request: %w", err)
	}
	req2.Header.Set("Authorization", "Bearer "+loginResp.Token)
	req2.Header.Set("Content-Type", "application/json")

	resp2, err := c.httpClient.Do(req2)
	if err != nil {
		return "", fmt.Errorf("send create-token request: %w", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusCreated {
		data, _ := io.ReadAll(io.LimitReader(resp2.Body, 300))
		return "", fmt.Errorf("create-token http %d: %s", resp2.StatusCode, strings.TrimSpace(string(data)))
	}

	var tokenResp struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}
	if tokenResp.Token == "" {
		return "", fmt.Errorf("empty API token in response")
	}

	return tokenResp.Token, nil
}

func (c *Client) SyncRecord(ctx context.Context, record Record) (string, error) {
	ack, err := c.SyncRecords(ctx, SyncBatch{StatusLogs: []Record{record}})
	if err != nil {
		return "", err
	}
	return ack.Detail, nil
}

func (c *Client) SyncRecords(ctx context.Context, batch SyncBatch) (SyncAck, error) {
	if !c.CanSync() {
		return SyncAck{}, fmt.Errorf("INTEGLIFE_API_TOKEN not set")
	}

	payload := buildSyncBatchPayload(batch)
	body, err := json.Marshal(payload)
	if err != nil {
		return SyncAck{}, fmt.Errorf("marshal sync payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL+"/api/notes/sync", bytes.NewReader(body))
	if err != nil {
		return SyncAck{}, fmt.Errorf("build sync request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return SyncAck{}, fmt.Errorf("send sync request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return SyncAck{}, fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	var syncResp syncResponse
	if err := json.NewDecoder(resp.Body).Decode(&syncResp); err != nil {
		return SyncAck{}, fmt.Errorf("decode sync response: %w", err)
	}
	return buildSyncAck(batch, syncResp, resp.Status), nil
}

type syncPayload struct {
	SyncModels        []string           `json:"sync_models"`
	LastSyncAtByModel map[string]*string `json:"last_sync_at_by_model"`
	Notes             []any              `json:"notes"`
	Comments          []any              `json:"comments"`
	MoneyCurrencies   []any              `json:"money_currencies"`
	MoneyTransactions []any              `json:"money_transactions"`
	MoneyBooks        []any              `json:"money_books"`
	MoneyBookMembers  []any              `json:"money_book_members"`
	Goals             []any              `json:"goals"`
	GoalMilestones    []any              `json:"goal_milestones"`
	GoalCheckins      []any              `json:"goal_checkins"`
	Todos             []any              `json:"todos"`
	Events            []any              `json:"events"`
	Pomodoros         []any              `json:"pomodoros"`
	StatusLogs        []syncStatusLog    `json:"status_logs"`
	AITaskRuns        []syncAITaskRun    `json:"ai_task_runs"`
	AITaskEvents      []syncAITaskEvent  `json:"ai_task_events"`
}

type syncStatusLog struct {
	UUID      string `json:"uuid"`
	LoggedAt  string `json:"logged_at"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	UserID    int    `json:"user_id"`
	LogType   string `json:"log_type"`
	Content   string `json:"content"`
}

func buildSyncPayload(record Record) syncPayload {
	return buildSyncBatchPayload(SyncBatch{StatusLogs: []Record{record}})
}

type syncAITaskRun struct {
	UUID                string  `json:"uuid"`
	UserID              int     `json:"user_id"`
	BookID              int     `json:"book_id"`
	ProjectType         string  `json:"project_type"`
	ProjectUUID         string  `json:"project_uuid"`
	TodoUUID            string  `json:"todo_uuid"`
	ParentRunUUID       string  `json:"parent_run_uuid"`
	AgentName           string  `json:"agent_name"`
	Status              string  `json:"status"`
	Title               string  `json:"title"`
	LatestPhase         string  `json:"latest_phase"`
	LatestSummary       string  `json:"latest_summary"`
	ContextSnapshotJSON string  `json:"context_snapshot_json"`
	StartedAt           *string `json:"started_at,omitempty"`
	LastHeartbeatAt     *string `json:"last_heartbeat_at,omitempty"`
	CompletedAt         *string `json:"completed_at,omitempty"`
	DeletedAt           *string `json:"deleted_at,omitempty"`
	CreatedAt           string  `json:"created_at"`
	UpdatedAt           string  `json:"updated_at"`
	ClientCreatedAt     string  `json:"client_created_at"`
	ClientUpdatedAt     string  `json:"client_updated_at"`
}

type syncAITaskEvent struct {
	UUID               string `json:"uuid"`
	UserID             int    `json:"user_id"`
	BookID             int    `json:"book_id"`
	RunUUID            string `json:"run_uuid"`
	TodoUUID           string `json:"todo_uuid"`
	EventType          string `json:"event_type"`
	Severity           string `json:"severity"`
	Title              string `json:"title"`
	Content            string `json:"content"`
	MetadataJSON       string `json:"metadata_json"`
	PayloadHashVersion int    `json:"payload_hash_version"`
	PayloadHash        string `json:"payload_hash"`
	OccurredAt         string `json:"occurred_at"`
	CreatedAt          string `json:"created_at"`
	UpdatedAt          string `json:"updated_at"`
	ClientCreatedAt    string `json:"client_created_at"`
	ClientUpdatedAt    string `json:"client_updated_at"`
}

type syncResponse struct {
	ServerTime             string            `json:"server_time"`
	AITaskRunsServerNewer  []syncResponseRun `json:"ai_task_runs_server_newer"`
	AITaskRunsOnlyOnServer []syncResponseRun `json:"ai_task_runs_only_on_server"`
	AITaskEventsAccepted   []string          `json:"ai_task_events_accepted"`
	AITaskEventsDuplicate  []string          `json:"ai_task_events_duplicate"`
	AITaskEventsRejected   []syncEventReject `json:"ai_task_events_rejected"`
}

type syncResponseRun struct {
	UUID string `json:"uuid"`
}

type syncEventReject struct {
	UUID   string `json:"uuid"`
	Reason string `json:"reason"`
}

func buildSyncBatchPayload(batch SyncBatch) syncPayload {
	models := []string{}
	if len(batch.StatusLogs) > 0 {
		models = append(models, "status_logs")
	}
	if len(batch.AITaskRuns) > 0 {
		models = append(models, "ai_task_runs")
	}
	if len(batch.AITaskEvents) > 0 {
		models = append(models, "ai_task_events")
	}
	if len(models) == 0 {
		models = []string{"status_logs", "ai_task_runs", "ai_task_events"}
	}
	lastSyncAtByModel := map[string]*string{}
	for _, model := range models {
		if model == "status_logs" {
			lastSyncAtByModel[model] = nil
			continue
		}
		if cursor, ok := batch.Cursors[model]; ok && !cursor.IsZero() {
			formatted := cursor.UTC().Format(time.RFC3339Nano)
			lastSyncAtByModel[model] = &formatted
		} else {
			formatted := time.Now().UTC().Format(time.RFC3339Nano)
			lastSyncAtByModel[model] = &formatted
		}
	}

	statusLogs := make([]syncStatusLog, 0, len(batch.StatusLogs))
	for _, record := range batch.StatusLogs {
		statusLogs = append(statusLogs, syncStatusLog{
			UUID:      record.UUID,
			LoggedAt:  record.LoggedAt.UTC().Format(time.RFC3339Nano),
			CreatedAt: record.CreatedAt.UTC().Format(time.RFC3339Nano),
			UpdatedAt: record.UpdatedAt.UTC().Format(time.RFC3339Nano),
			UserID:    0,
			LogType:   record.LogType,
			Content:   record.Content,
		})
	}

	runs := make([]syncAITaskRun, 0, len(batch.AITaskRuns))
	for _, run := range batch.AITaskRuns {
		runs = append(runs, syncAITaskRun{
			UUID:                run.UUID,
			UserID:              0,
			BookID:              0,
			ProjectType:         run.ProjectType,
			ProjectUUID:         run.ProjectUUID,
			TodoUUID:            run.TodoUUID,
			ParentRunUUID:       run.ParentRunUUID,
			AgentName:           run.AgentName,
			Status:              run.Status,
			Title:               run.Title,
			LatestPhase:         run.LatestPhase,
			LatestSummary:       run.LatestSummary,
			ContextSnapshotJSON: emptyJSON(run.ContextSnapshotJSON),
			StartedAt:           timePtrString(run.StartedAt),
			LastHeartbeatAt:     timePtrString(run.LastHeartbeatAt),
			CompletedAt:         timePtrString(run.CompletedAt),
			DeletedAt:           timePtrString(run.DeletedAt),
			CreatedAt:           run.CreatedAt.UTC().Format(time.RFC3339Nano),
			UpdatedAt:           run.UpdatedAt.UTC().Format(time.RFC3339Nano),
			ClientCreatedAt:     run.ClientCreatedAt.UTC().Format(time.RFC3339Nano),
			ClientUpdatedAt:     run.ClientUpdatedAt.UTC().Format(time.RFC3339Nano),
		})
	}

	events := make([]syncAITaskEvent, 0, len(batch.AITaskEvents))
	for _, event := range batch.AITaskEvents {
		events = append(events, syncAITaskEvent{
			UUID:               event.UUID,
			UserID:             0,
			BookID:             0,
			RunUUID:            event.RunUUID,
			TodoUUID:           event.TodoUUID,
			EventType:          event.EventType,
			Severity:           event.Severity,
			Title:              event.Title,
			Content:            event.Content,
			MetadataJSON:       emptyJSON(event.MetadataJSON),
			PayloadHashVersion: event.PayloadHashVersion,
			PayloadHash:        event.PayloadHash,
			OccurredAt:         event.OccurredAt.UTC().Format(time.RFC3339Nano),
			CreatedAt:          event.CreatedAt.UTC().Format(time.RFC3339Nano),
			UpdatedAt:          event.UpdatedAt.UTC().Format(time.RFC3339Nano),
			ClientCreatedAt:    event.ClientCreatedAt.UTC().Format(time.RFC3339Nano),
			ClientUpdatedAt:    event.ClientUpdatedAt.UTC().Format(time.RFC3339Nano),
		})
	}

	return syncPayload{
		SyncModels:        models,
		LastSyncAtByModel: lastSyncAtByModel,
		Notes:             []any{},
		Comments:          []any{},
		MoneyCurrencies:   []any{},
		MoneyTransactions: []any{},
		MoneyBooks:        []any{},
		MoneyBookMembers:  []any{},
		Goals:             []any{},
		GoalMilestones:    []any{},
		GoalCheckins:      []any{},
		Todos:             []any{},
		Events:            []any{},
		Pomodoros:         []any{},
		StatusLogs:        statusLogs,
		AITaskRuns:        runs,
		AITaskEvents:      events,
	}
}

func buildSyncAck(batch SyncBatch, resp syncResponse, detail string) SyncAck {
	serverTime := time.Now().UTC()
	if resp.ServerTime != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, resp.ServerTime); err == nil {
			serverTime = parsed.UTC()
		}
	}

	runConflicts := map[string]bool{}
	for _, run := range resp.AITaskRunsServerNewer {
		runConflicts[run.UUID] = true
	}
	ack := SyncAck{
		StatusLogUUIDs:    make([]string, 0, len(batch.StatusLogs)),
		AITaskRunSynced:   []string{},
		AITaskEventSynced: []string{},
		AITaskEventErrors: map[string]string{},
		ServerTime:        serverTime,
		Detail:            detail,
	}
	for _, record := range batch.StatusLogs {
		ack.StatusLogUUIDs = append(ack.StatusLogUUIDs, record.UUID)
	}
	for _, run := range batch.AITaskRuns {
		if runConflicts[run.UUID] {
			ack.AITaskRunConflicts = append(ack.AITaskRunConflicts, run.UUID)
			continue
		}
		ack.AITaskRunSynced = append(ack.AITaskRunSynced, run.UUID)
	}

	eventOK := map[string]bool{}
	for _, uuid := range resp.AITaskEventsAccepted {
		eventOK[uuid] = true
	}
	for _, uuid := range resp.AITaskEventsDuplicate {
		eventOK[uuid] = true
	}
	for _, reject := range resp.AITaskEventsRejected {
		ack.AITaskEventErrors[reject.UUID] = reject.Reason
	}
	for _, event := range batch.AITaskEvents {
		switch {
		case eventOK[event.UUID]:
			ack.AITaskEventSynced = append(ack.AITaskEventSynced, event.UUID)
		case ack.AITaskEventErrors[event.UUID] != "":
			// Keep pending with the server-provided reason.
		default:
			ack.AITaskEventErrors[event.UUID] = "missing event ack"
		}
	}
	return ack
}

func timePtrString(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339Nano)
	return &formatted
}
