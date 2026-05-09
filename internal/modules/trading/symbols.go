package trading

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/tiennm99/miti99bot-go/internal/storage"
)

// tickerRe restricts tickers to ASCII alphanumeric, 1-16 chars. Stops
// Cyrillic / unicode-lookalike inputs from amplifying KBS lookups, and
// guards the cache key alphabet (sym:<TICKER>) from oddities.
var tickerRe = regexp.MustCompile(`^[A-Z0-9]{1,16}$`)

// ResolvedSymbol is the cached entry written under "sym:<TICKER>". Category
// is currently always "stock" — crypto/gold/forex are upstream future-work.
type ResolvedSymbol struct {
	Symbol   string `json:"symbol"`
	Category string `json:"category"`
	Label    string `json:"label"`
}

// ErrUnknownTicker means KBS has no price data for the given ticker — i.e.
// the symbol is not a tradeable VN stock as far as our source is concerned.
var ErrUnknownTicker = errors.New("trading: unknown ticker")

// ResolveSymbol returns the cached ResolvedSymbol if any, otherwise queries
// KBS to validate the ticker and caches the result permanently. Tickers
// don't change; permanent caching is correct.
//
// The empty-input case returns ErrUnknownTicker to keep the caller's branch
// shape simple (one error path covers both empty + unknown).
func ResolveSymbol(ctx context.Context, kv storage.KVStore, prices *PriceClient, ticker string) (ResolvedSymbol, error) {
	ticker = strings.ToUpper(strings.TrimSpace(ticker))
	if !tickerRe.MatchString(ticker) {
		return ResolvedSymbol{}, ErrUnknownTicker
	}
	cacheKey := "sym:" + ticker

	var cached ResolvedSymbol
	if err := kv.GetJSON(ctx, cacheKey, &cached); err == nil {
		return cached, nil
	} else if !errors.Is(err, storage.ErrNotFound) {
		return ResolvedSymbol{}, fmt.Errorf("trading: cache read %s: %w", ticker, err)
	}

	// Cache miss → validate against KBS by attempting a price fetch.
	if _, err := prices.FetchPrice(ctx, ticker); err != nil {
		if errors.Is(err, ErrNoPrice) {
			return ResolvedSymbol{}, ErrUnknownTicker
		}
		return ResolvedSymbol{}, err
	}

	resolved := ResolvedSymbol{Symbol: ticker, Category: "stock", Label: ticker}
	// Cache write failure is non-fatal — next call will resolve again.
	_ = kv.PutJSON(ctx, cacheKey, resolved)
	return resolved, nil
}
