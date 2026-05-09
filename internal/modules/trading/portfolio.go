package trading

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/tiennm99/miti99bot-go/internal/storage"
)

// Portfolio is the per-user trading state. Currency is a map for forward-
// compat with USD/EUR (currently VND-only). Assets is a flat ticker→qty map
// — category lives in the symbol cache, not the portfolio.
type Portfolio struct {
	Currency map[string]float64 `json:"currency"`
	Assets   map[string]int64   `json:"assets"`
	Meta     PortfolioMeta      `json:"meta"`
}

// PortfolioMeta tracks invested cost basis for P&L. CreatedAt is purely
// informational; carried for parity with JS schema.
type PortfolioMeta struct {
	Invested  float64 `json:"invested"`
	CreatedAt int64   `json:"createdAt"`
}

// NewPortfolio returns an empty starting state. Currency map seeded with VND=0
// so deductCurrency on a fresh user reports "0 balance" cleanly instead of
// nil-map panics.
func NewPortfolio(now int64) Portfolio {
	return Portfolio{
		Currency: map[string]float64{"VND": 0},
		Assets:   map[string]int64{},
		Meta:     PortfolioMeta{Invested: 0, CreatedAt: now},
	}
}

func portfolioKey(userID int64) string {
	return "user:" + strconv.FormatInt(userID, 10)
}

// LoadPortfolio reads from KV; returns an empty portfolio on first-time use.
// Defensively initialises nil maps so callers never need a nil check.
func LoadPortfolio(ctx context.Context, kv storage.KVStore, userID int64, now int64) (Portfolio, error) {
	var p Portfolio
	err := kv.GetJSON(ctx, portfolioKey(userID), &p)
	switch {
	case err == nil:
		// Repair any nils from older / partial saves — defence in depth.
		if p.Currency == nil {
			p.Currency = map[string]float64{"VND": 0}
		} else if _, ok := p.Currency["VND"]; !ok {
			p.Currency["VND"] = 0
		}
		if p.Assets == nil {
			p.Assets = map[string]int64{}
		}
		return p, nil
	case errors.Is(err, storage.ErrNotFound):
		return NewPortfolio(now), nil
	default:
		return Portfolio{}, fmt.Errorf("trading: load portfolio %d: %w", userID, err)
	}
}

// SavePortfolio persists the portfolio.
func SavePortfolio(ctx context.Context, kv storage.KVStore, userID int64, p Portfolio) error {
	if err := kv.PutJSON(ctx, portfolioKey(userID), p); err != nil {
		return fmt.Errorf("trading: save portfolio %d: %w", userID, err)
	}
	return nil
}

// AddCurrency credits the currency balance.
func (p *Portfolio) AddCurrency(currency string, amount float64) {
	if p.Currency == nil {
		p.Currency = map[string]float64{}
	}
	p.Currency[currency] += amount
}

// DeductCurrency debits the currency balance. Returns false + the current
// balance when insufficient — caller renders the user-facing error.
func (p *Portfolio) DeductCurrency(currency string, amount float64) (ok bool, balance float64) {
	if p.Currency == nil {
		p.Currency = map[string]float64{}
	}
	balance = p.Currency[currency]
	if balance < amount {
		return false, balance
	}
	p.Currency[currency] = balance - amount
	return true, p.Currency[currency]
}

// AddAsset credits the share holding.
func (p *Portfolio) AddAsset(symbol string, qty int64) {
	if p.Assets == nil {
		p.Assets = map[string]int64{}
	}
	p.Assets[symbol] += qty
}

// DeductAsset debits the share holding. Returns false + held when caller asks
// for more than they own. Removes the key when balance hits zero so the
// portfolio doesn't accumulate empty entries.
func (p *Portfolio) DeductAsset(symbol string, qty int64) (ok bool, held int64) {
	if p.Assets == nil {
		p.Assets = map[string]int64{}
	}
	held = p.Assets[symbol]
	if held < qty {
		return false, held
	}
	remaining := held - qty
	if remaining == 0 {
		delete(p.Assets, symbol)
	} else {
		p.Assets[symbol] = remaining
	}
	return true, remaining
}
