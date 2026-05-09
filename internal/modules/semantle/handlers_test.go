package semantle

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tiennm99/miti99bot-go/internal/ai"
	"github.com/tiennm99/miti99bot-go/internal/modules"
	"github.com/tiennm99/miti99bot-go/internal/storage"
	"github.com/tiennm99/miti99bot-go/internal/testutil"
)

// fakeEmbedder always returns deterministic vectors so cosine math is testable.
type fakeEmbedder struct {
	vecs map[string][]float32
	err  error
}

func (f *fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([][]float32, len(texts))
	for i, t := range texts {
		if v, ok := f.vecs[t]; ok {
			out[i] = v
			continue
		}
		// Default: distinct unit vector per text — orthogonal pairs score 0.
		v := make([]float32, 8)
		v[len(t)%len(v)] = 1
		out[i] = v
	}
	return out, nil
}

func install(t *testing.T, embedder ai.Embedder) (*testutil.RecordingBot, *modules.Registry) {
	t.Helper()
	rb := testutil.NewRecordingBot(t)
	reg, err := modules.Build([]string{"semantle"},
		map[string]modules.Factory{"semantle": New},
		storage.NewMemoryProvider(), nil,
		modules.BuildOptions{Embedder: embedder})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	modules.Install(rb.Bot, reg, modules.Auth{})
	return rb, reg
}

func TestSemantle_NoEmbedderRefuses(t *testing.T) {
	rb, _ := install(t, nil)
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(42, "/semantle"))
	last := rb.LastSent().Text()
	if !strings.Contains(last, "GEMINI_API_KEY") {
		t.Errorf("missing-key warning: got %q", last)
	}
}

func TestSemantle_BoardOnEmptyArg(t *testing.T) {
	rb, _ := install(t, &fakeEmbedder{})
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(42, "/semantle"))
	last := rb.LastSent().Text()
	if !strings.Contains(last, "Semantle") {
		t.Errorf("board-render: got %q", last)
	}
}

func TestSemantle_OOVRejected(t *testing.T) {
	rb, _ := install(t, &fakeEmbedder{})
	// "qzwxyz" is not in the embedded wordlist → OOV reply, no upstream call.
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(42, "/semantle qzwxyz"))
	last := rb.LastSent().Text()
	if !strings.Contains(last, "vocabulary") {
		t.Errorf("OOV reply: got %q", last)
	}
}

func TestSemantle_RateLimitedReply(t *testing.T) {
	rb, _ := install(t, &fakeEmbedder{err: ai.ErrRateLimited})
	// Use a real vocab word so we get past the OOV gate.
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(42, "/semantle the"))
	last := rb.LastSent().Text()
	if !strings.Contains(last, "rate-limited") {
		t.Errorf("rate-limit reply: got %q", last)
	}
}

func TestSemantle_UpstreamFail(t *testing.T) {
	rb, _ := install(t, &fakeEmbedder{err: errors.New("boom")})
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(42, "/semantle the"))
	last := rb.LastSent().Text()
	if !strings.Contains(last, "Upstream hiccup") {
		t.Errorf("upstream reply: got %q", last)
	}
}

func TestSemantle_GiveupNoActiveRound(t *testing.T) {
	rb, _ := install(t, &fakeEmbedder{})
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(42, "/semantle_giveup"))
	last := rb.LastSent().Text()
	if !strings.Contains(last, "No active round") {
		t.Errorf("giveup-no-round: got %q", last)
	}
}

func TestSemantle_StatsEmpty(t *testing.T) {
	rb, _ := install(t, &fakeEmbedder{})
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(42, "/semantle_stats"))
	last := rb.LastSent().Text()
	if !strings.Contains(last, "No semantle games") {
		t.Errorf("empty-stats: got %q", last)
	}
}
