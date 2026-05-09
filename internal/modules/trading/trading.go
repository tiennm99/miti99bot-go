package trading

import (
	"github.com/tiennm99/miti99bot-go/internal/modules"
)

// New is the trading module Factory. Five user-facing commands; no crons.
// (Original miti99bot only has a SQL retention cron, which our KV-only port
// does not implement — keeping commits paper-ledger-only is acceptable.)
func New(deps modules.Deps) modules.Module {
	s := newState(deps.KV)
	return modules.Module{
		Commands: []modules.Command{
			{
				Name:        "trade_topup",
				Visibility:  modules.VisibilityPublic,
				Description: "Top up VND to your trading account",
				Handler:     s.handleTopup,
			},
			{
				Name:        "trade_buy",
				Visibility:  modules.VisibilityPublic,
				Description: "Buy VN stock at market price (qty TICKER)",
				Handler:     s.handleBuy,
			},
			{
				Name:        "trade_sell",
				Visibility:  modules.VisibilityPublic,
				Description: "Sell VN stock back to VND (qty TICKER)",
				Handler:     s.handleSell,
			},
			{
				Name:        "trade_convert",
				Visibility:  modules.VisibilityPublic,
				Description: "Currency exchange (coming soon)",
				Handler:     s.handleConvert,
			},
			{
				Name:        "trade_stats",
				Visibility:  modules.VisibilityPublic,
				Description: "Show portfolio summary with P&L",
				Handler:     s.handleStats,
			},
		},
	}
}
