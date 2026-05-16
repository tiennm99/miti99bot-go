package migration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// CloudflareD1Client is a thin read-only REST client for the legacy
// Cloudflare D1 database. The migration only uses it for the optional
// trading_trades audit dump in Phase 03; runtime data flow never reads D1.
type CloudflareD1Client struct {
	httpClient *http.Client
	apiBase    string
	accountID  string
	databaseID string
	apiToken   string
}

func NewCloudflareD1Client(accountID, databaseID, apiToken string) *CloudflareD1Client {
	if accountID == "" || databaseID == "" || apiToken == "" {
		panic("migration: CloudflareD1Client requires accountID, databaseID, apiToken")
	}
	return &CloudflareD1Client{
		httpClient: &http.Client{Timeout: 60 * time.Second},
		apiBase:    "https://api.cloudflare.com/client/v4",
		accountID:  accountID,
		databaseID: databaseID,
		apiToken:   apiToken,
	}
}

func (c *CloudflareD1Client) SetBaseURL(base string) { c.apiBase = base }

// d1QueryEnvelope matches the D1 query response. Result is an array because
// D1 returns one entry per statement; we only ever send one.
type d1QueryEnvelope struct {
	Result []struct {
		Results []map[string]any `json:"results"`
		Success bool             `json:"success"`
	} `json:"result"`
	Success bool             `json:"success"`
	Errors  []map[string]any `json:"errors"`
}

// Query runs a single SQL statement and returns the row maps in result-set
// order. params is optional; pass nil for parameterless statements.
func (c *CloudflareD1Client) Query(ctx context.Context, sql string, params []any) ([]map[string]any, error) {
	payload := map[string]any{"sql": sql}
	if len(params) > 0 {
		payload["params"] = params
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	endpoint := fmt.Sprintf("%s/accounts/%s/d1/database/%s/query",
		c.apiBase, c.accountID, c.databaseID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("d1 query: status %d: %s", resp.StatusCode, string(raw))
	}
	var env d1QueryEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("d1 query: decode: %w", err)
	}
	if !env.Success {
		return nil, fmt.Errorf("d1 query: api error: %v", env.Errors)
	}
	if len(env.Result) == 0 {
		return nil, nil
	}
	return env.Result[0].Results, nil
}
