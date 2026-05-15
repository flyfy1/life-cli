package statuslog

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
	if !c.CanSync() {
		return "", fmt.Errorf("STATUSLOG_API_TOKEN not set")
	}

	body, err := json.Marshal(buildSyncPayload(record))
	if err != nil {
		return "", fmt.Errorf("marshal sync payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL+"/api/notes/sync", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build sync request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("send sync request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return "", fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	return resp.Status, nil
}

type syncPayload struct {
	SyncModels        []string        `json:"sync_models"`
	Notes             []any           `json:"notes"`
	Comments          []any           `json:"comments"`
	MoneyCurrencies   []any           `json:"money_currencies"`
	MoneyTransactions []any           `json:"money_transactions"`
	MoneyBooks        []any           `json:"money_books"`
	MoneyBookMembers  []any           `json:"money_book_members"`
	Goals             []any           `json:"goals"`
	GoalMilestones    []any           `json:"goal_milestones"`
	GoalCheckins      []any           `json:"goal_checkins"`
	Todos             []any           `json:"todos"`
	Events            []any           `json:"events"`
	Pomodoros         []any           `json:"pomodoros"`
	StatusLogs        []syncStatusLog `json:"status_logs"`
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
	return syncPayload{
		SyncModels:        []string{"status_logs"},
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
		StatusLogs: []syncStatusLog{
			{
				UUID:      record.UUID,
				LoggedAt:  record.LoggedAt.UTC().Format(time.RFC3339Nano),
				CreatedAt: record.CreatedAt.UTC().Format(time.RFC3339Nano),
				UpdatedAt: record.UpdatedAt.UTC().Format(time.RFC3339Nano),
				UserID:    0,
				LogType:   record.LogType,
				Content:   record.Content,
			},
		},
	}
}
