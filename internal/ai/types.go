// Package ai wraps Google's genai SDK with a small, mockable surface for the
// semantle/twentyq modules. The package owns:
//
//   - Embedder / Chatter interfaces — what modules consume
//   - Client struct           — production implementation backed by genai
//   - per-user rate limiter   — defends the shared 1500-RPD Gemini free tier
//
// Modules accept the interfaces (not *Client) so unit tests can pass fakes
// without spinning up a real Gemini client. Production wiring in cmd/server
// passes the *Client (which satisfies both interfaces) into Deps.
package ai

import "context"

// Embedder produces dense vectors for text. Used by the semantle module to
// score guess similarity. Implementations must respect ctx cancellation.
//
// On rate-limit (HTTP 429) the implementation should return a sentinel error
// — see ErrRateLimited — so callers can show a user-friendly retry message.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// Chatter produces a single text completion from a system + user prompt
// pair. Used by the twentyq module's judge + round-start calls. Same
// rate-limit conventions as Embedder.
type Chatter interface {
	Generate(ctx context.Context, system, user string) (string, error)
}
