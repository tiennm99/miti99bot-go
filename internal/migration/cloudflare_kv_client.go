package migration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// CloudflareKVClient is a thin read-only REST client for the legacy
// Cloudflare Workers KV namespace. List+Get are the only operations the
// migration needs; mutations stay out of scope.
type CloudflareKVClient struct {
	httpClient  *http.Client
	apiBase     string
	accountID   string
	namespaceID string
	apiToken    string
}

// NewCloudflareKVClient panics on empty required fields so misconfiguration
// surfaces at startup rather than after partial work.
func NewCloudflareKVClient(accountID, namespaceID, apiToken string) *CloudflareKVClient {
	if accountID == "" || namespaceID == "" || apiToken == "" {
		panic("migration: CloudflareKVClient requires accountID, namespaceID, apiToken")
	}
	return &CloudflareKVClient{
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		apiBase:     "https://api.cloudflare.com/client/v4",
		accountID:   accountID,
		namespaceID: namespaceID,
		apiToken:    apiToken,
	}
}

// SetBaseURL overrides the API base; used by tests with httptest.Server.
func (c *CloudflareKVClient) SetBaseURL(base string) { c.apiBase = base }

// kvListEnvelope matches the Cloudflare REST list-keys response shape.
// See: https://developers.cloudflare.com/api/operations/workers-kv-namespace-list-a-namespace-s-keys
type kvListEnvelope struct {
	Result []struct {
		Name string `json:"name"`
	} `json:"result"`
	Success    bool             `json:"success"`
	Errors     []map[string]any `json:"errors"`
	ResultInfo struct {
		Cursor string `json:"cursor"`
		Count  int    `json:"count"`
	} `json:"result_info"`
}

// ListKeys returns every key in the namespace. The Phase 01 inventory
// proved the namespace fits in well under one REST page (21 keys vs 1000
// page limit), but pagination is still honored for safety.
func (c *CloudflareKVClient) ListKeys(ctx context.Context) ([]string, error) {
	var out []string
	cursor := ""
	for {
		page, next, err := c.listPage(ctx, cursor)
		if err != nil {
			return nil, err
		}
		out = append(out, page...)
		if next == "" {
			return out, nil
		}
		cursor = next
	}
}

func (c *CloudflareKVClient) listPage(ctx context.Context, cursor string) ([]string, string, error) {
	endpoint := fmt.Sprintf("%s/accounts/%s/storage/kv/namespaces/%s/keys",
		c.apiBase, c.accountID, c.namespaceID)
	if cursor != "" {
		endpoint += "?" + url.Values{"cursor": []string{cursor}}.Encode()
	}
	body, err := c.do(ctx, http.MethodGet, endpoint)
	if err != nil {
		return nil, "", err
	}
	var env kvListEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, "", fmt.Errorf("kv list: decode: %w", err)
	}
	if !env.Success {
		return nil, "", fmt.Errorf("kv list: api error: %v", env.Errors)
	}
	names := make([]string, 0, len(env.Result))
	for _, r := range env.Result {
		names = append(names, r.Name)
	}
	return names, env.ResultInfo.Cursor, nil
}

// GetValue returns the raw value bytes for one KV key. CF returns 404 as
// errKeyNotFound so callers can decide whether a missing key is fatal.
func (c *CloudflareKVClient) GetValue(ctx context.Context, key string) ([]byte, error) {
	endpoint := fmt.Sprintf("%s/accounts/%s/storage/kv/namespaces/%s/values/%s",
		c.apiBase, c.accountID, c.namespaceID, url.PathEscape(key))
	return c.do(ctx, http.MethodGet, endpoint)
}

// ErrKeyNotFound signals a 404 on a value GET. Returned wrapped so callers
// can use errors.Is.
var ErrKeyNotFound = errors.New("cloudflare kv: key not found")

func (c *CloudflareKVClient) do(ctx context.Context, method, endpoint string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w: %s", ErrKeyNotFound, endpoint)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("cloudflare api %s %s: status %d: %s",
			method, endpoint, resp.StatusCode, string(body))
	}
	return body, nil
}
