package ai

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"google.golang.org/genai"
)

// chatModel is pinned here rather than exposed to callers — modules should
// not pick their own model. If we ever need to A/B test, a higher-level
// config wins, not a per-module override.
const chatModel = "gemini-2.5-flash" // newest flash; 15 RPM / 1500 RPD free

// ErrRateLimited is returned when the upstream rejected with 429 (or our
// in-process per-user bucket dropped the call). Modules show a friendly
// "AI is rate-limited, try again in N minutes" message on this sentinel.
var ErrRateLimited = errors.New("ai: rate limited")

// ErrNotConfigured is returned when GEMINI_API_KEY was empty at startup.
// The Client is nil in that case; modules using AI must check before
// invoking and refuse the command with a config-error reply.
var ErrNotConfigured = errors.New("ai: GEMINI_API_KEY not set")

// Client wraps a *genai.Client with the small surface the bot needs. The
// underlying gRPC connection is reused across requests — Lambda cold-start
// budget makes a per-request handshake intolerable.
//
// Safe for concurrent use; *genai.Client is itself goroutine-safe.
type Client struct {
	g *genai.Client
}

// NewClient constructs a *Client backed by the Gemini API (not Vertex AI —
// Vertex requires a service-account flow incompatible with the free-tier
// Lambda baseline). A blank apiKey returns ErrNotConfigured so callers
// can decide whether to skip AI-dependent module loading.
func NewClient(ctx context.Context, apiKey string) (*Client, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, ErrNotConfigured
	}
	g, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("ai: genai.NewClient: %w", err)
	}
	return &Client{g: g}, nil
}

// Generate runs a single-turn chat with `system` as the system instruction
// and `user` as the user message. Returns the model's text reply.
//
// The output cap matches what the JS twentyq prompt expects (≤200 tokens,
// single-line JSON). Temperature 0.7 mirrors the JS code path.
func (c *Client) Generate(ctx context.Context, system, user string) (string, error) {
	if c == nil || c.g == nil {
		return "", ErrNotConfigured
	}
	cfg := &genai.GenerateContentConfig{
		Temperature:     ptrFloat32(0.7),
		MaxOutputTokens: 256,
	}
	if system != "" {
		cfg.SystemInstruction = genai.NewContentFromText(system, genai.RoleUser)
	}
	resp, err := c.g.Models.GenerateContent(ctx, chatModel, genai.Text(user), cfg)
	if err != nil {
		if isRateLimit(err) {
			return "", ErrRateLimited
		}
		return "", fmt.Errorf("ai: GenerateContent: %w", err)
	}
	if resp == nil {
		return "", fmt.Errorf("ai: GenerateContent: nil response")
	}
	return resp.Text(), nil
}

func ptrFloat32(v float32) *float32 { return &v }

// isRateLimit returns true if err looks like a Gemini 429. The genai SDK
// surfaces 429 as a typed error with Code=429 in some paths and as a wrapped
// HTTP status string in others — we sniff both.
func isRateLimit(err error) bool {
	if err == nil {
		return false
	}
	var apiErr genai.APIError
	if errors.As(err, &apiErr) && apiErr.Code == 429 {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "429") || strings.Contains(strings.ToLower(msg), "resource_exhausted")
}
