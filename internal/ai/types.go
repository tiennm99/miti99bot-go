// Package ai wraps Google's genai SDK with a small, mockable surface for the
// twentyq module. The package owns:
//
//   - Chatter interface      — what modules consume
//   - Client struct           — production implementation backed by genai
//   - per-user rate limiter   — defends the shared 1500-RPD Gemini free tier
//
// Modules accept the interface (not *Client) so unit tests can pass fakes
// without spinning up a real Gemini client. Production wiring in cmd/server
// passes the *Client (which satisfies the interface) into Deps.
package ai

import "context"

// Chatter produces a single text completion from a system + user prompt
// pair. Used by the twentyq module's judge + round-start calls.
//
// On rate-limit (HTTP 429) the implementation should return ErrRateLimited
// so callers can show a user-friendly retry message.
type Chatter interface {
	Generate(ctx context.Context, system, user string) (string, error)
}
