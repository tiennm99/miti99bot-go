package trading

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/tiennm99/miti99bot-go/internal/keylock"
	"github.com/tiennm99/miti99bot-go/internal/log"
	"github.com/tiennm99/miti99bot-go/internal/modules/util/chathelper"
	"github.com/tiennm99/miti99bot-go/internal/storage"
)

// state is the per-module runtime. KV is module-scoped (the framework
// prefixes/partitions). PriceClient is reused across calls; nowFn allows
// tests to inject a deterministic clock for portfolio CreatedAt.
type state struct {
	kv                storage.KVStore
	prices            *PriceClient
	locks             keylock.Map
	nowFn             func() time.Time
	comingSoonMessage string // exposed for tests / future i18n
}

func (s *state) now() time.Time {
	if s.nowFn != nil {
		return s.nowFn()
	}
	return time.Now()
}

// newState builds the default state used by the module factory.
func newState(kv storage.KVStore) *state {
	return &state{
		kv:                kv,
		prices:            &PriceClient{},
		comingSoonMessage: "Crypto, gold & currency exchange coming soon!",
	}
}

// senderInfo extracts the Telegram user ID for state-keying. Channel posts
// and inline queries lack a from-user; we refuse to operate without one
// because state under "user:0" would collide across all such updates.
// Defensive against From.ID == 0 (anonymized senders / future Telegram
// schema drift) for the same reason.
func senderInfo(update *models.Update) (userID int64, chatID int64, ok bool) {
	msg := update.Message
	if msg == nil || msg.From == nil || msg.From.ID == 0 {
		return 0, 0, false
	}
	return msg.From.ID, msg.Chat.ID, true
}

// argsAfterCommand splits the command body into whitespace-separated args.
// "/trade_buy 100 TCB" → ["100", "TCB"]; "/trade_topup" → []
func argsAfterCommand(text string) []string {
	parts := strings.Fields(text)
	if len(parts) <= 1 {
		return nil
	}
	return parts[1:]
}

func (s *state) handleTopup(ctx context.Context, b *bot.Bot, update *models.Update) error {
	userID, chatID, ok := senderInfo(update)
	if !ok {
		return chathelper.Reply(ctx, b, update.Message.Chat.ID,
			"Cannot identify user — trading only works in private/group chats with a sender.")
	}
	args := argsAfterCommand(update.Message.Text)
	if len(args) < 1 {
		return chathelper.Reply(ctx, b, chatID, "Usage: /trade_topup <amount>\nExample: /trade_topup 5000000")
	}
	amount, err := strconv.ParseFloat(args[0], 64)
	if err != nil || amount <= 0 {
		return chathelper.Reply(ctx, b, chatID, "Amount must be a positive number.")
	}

	defer s.locks.Acquire(strconv.FormatInt(userID, 10))()

	p, err := LoadPortfolio(ctx, s.kv, userID, s.now().UnixMilli())
	if err != nil {
		log.Error("trading_load_portfolio", "user", userID, "err", err)
		return chathelper.Reply(ctx, b, chatID, "Could not load portfolio. Try again later.")
	}
	p.AddCurrency("VND", amount)
	p.Meta.Invested += amount
	if err := SavePortfolio(ctx, s.kv, userID, p); err != nil {
		log.Error("trading_save_portfolio", "user", userID, "err", err)
		return chathelper.Reply(ctx, b, chatID, "Could not save portfolio. Try again later.")
	}
	return chathelper.Reply(ctx, b, chatID,
		"Topped up "+FormatVND(amount)+".\nBalance: "+FormatVND(p.Currency["VND"]))
}

func (s *state) handleBuy(ctx context.Context, b *bot.Bot, update *models.Update) error {
	userID, chatID, ok := senderInfo(update)
	if !ok {
		return chathelper.Reply(ctx, b, update.Message.Chat.ID,
			"Cannot identify user — trading only works in private/group chats with a sender.")
	}
	args := argsAfterCommand(update.Message.Text)
	if len(args) < 2 {
		return chathelper.Reply(ctx, b, chatID, "Usage: /trade_buy <qty> <TICKER>\nExample: /trade_buy 100 TCB")
	}
	qty, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || qty <= 0 {
		return chathelper.Reply(ctx, b, chatID, "Quantity must be a positive whole number.")
	}

	resolved, err := ResolveSymbol(ctx, s.kv, s.prices, args[1])
	if err != nil {
		if errors.Is(err, ErrUnknownTicker) {
			return chathelper.Reply(ctx, b, chatID,
				"Unknown stock ticker \""+strings.ToUpper(args[1])+"\".\n"+s.comingSoonMessage)
		}
		log.Error("trading_resolve_symbol", "ticker", args[1], "err", err)
		return chathelper.Reply(ctx, b, chatID, "Could not look up that ticker. Try again later.")
	}

	price, err := s.prices.FetchPrice(ctx, resolved.Symbol)
	if err != nil {
		if errors.Is(err, ErrNoPrice) {
			return chathelper.Reply(ctx, b, chatID, "No price available for "+resolved.Symbol+".")
		}
		log.Error("trading_fetch_price", "ticker", resolved.Symbol, "err", err)
		return chathelper.Reply(ctx, b, chatID, "Could not fetch price. Try again later.")
	}
	cost := float64(qty) * price

	defer s.locks.Acquire(strconv.FormatInt(userID, 10))()

	p, err := LoadPortfolio(ctx, s.kv, userID, s.now().UnixMilli())
	if err != nil {
		log.Error("trading_load_portfolio", "user", userID, "err", err)
		return chathelper.Reply(ctx, b, chatID, "Could not load portfolio. Try again later.")
	}
	ok, balance := p.DeductCurrency("VND", cost)
	if !ok {
		return chathelper.Reply(ctx, b, chatID,
			"Insufficient VND. Need "+FormatVND(cost)+", have "+FormatVND(balance)+".")
	}
	p.AddAsset(resolved.Symbol, qty)
	if err := SavePortfolio(ctx, s.kv, userID, p); err != nil {
		log.Error("trading_save_portfolio", "user", userID, "err", err)
		return chathelper.Reply(ctx, b, chatID, "Could not save portfolio. Try again later.")
	}
	return chathelper.Reply(ctx, b, chatID,
		"Bought "+FormatStock(float64(qty))+" "+resolved.Symbol+
			" @ "+FormatVND(price)+"\nCost: "+FormatVND(cost))
}

func (s *state) handleSell(ctx context.Context, b *bot.Bot, update *models.Update) error {
	userID, chatID, ok := senderInfo(update)
	if !ok {
		return chathelper.Reply(ctx, b, update.Message.Chat.ID,
			"Cannot identify user — trading only works in private/group chats with a sender.")
	}
	args := argsAfterCommand(update.Message.Text)
	if len(args) < 2 {
		return chathelper.Reply(ctx, b, chatID, "Usage: /trade_sell <qty> <TICKER>\nExample: /trade_sell 100 TCB")
	}
	qty, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || qty <= 0 {
		return chathelper.Reply(ctx, b, chatID, "Quantity must be a positive whole number.")
	}

	// Resolve + fetch price BEFORE taking the per-user lock. Mirrors handleBuy:
	// keeps the critical section to a fast Get→mutate→Put, and removes any need
	// for a rollback path (no in-memory mutation precedes the network call).
	resolved, err := ResolveSymbol(ctx, s.kv, s.prices, args[1])
	if err != nil {
		if errors.Is(err, ErrUnknownTicker) {
			return chathelper.Reply(ctx, b, chatID,
				"Unknown stock ticker \""+strings.ToUpper(args[1])+"\".")
		}
		log.Error("trading_resolve_symbol", "ticker", args[1], "err", err)
		return chathelper.Reply(ctx, b, chatID, "Could not look up that ticker. Try again later.")
	}
	price, err := s.prices.FetchPrice(ctx, resolved.Symbol)
	if err != nil {
		if errors.Is(err, ErrNoPrice) {
			return chathelper.Reply(ctx, b, chatID, "No price available for "+resolved.Symbol+".")
		}
		log.Error("trading_fetch_price", "ticker", resolved.Symbol, "err", err)
		return chathelper.Reply(ctx, b, chatID, "Could not fetch price. Try again later.")
	}

	defer s.locks.Acquire(strconv.FormatInt(userID, 10))()

	p, err := LoadPortfolio(ctx, s.kv, userID, s.now().UnixMilli())
	if err != nil {
		log.Error("trading_load_portfolio", "user", userID, "err", err)
		return chathelper.Reply(ctx, b, chatID, "Could not load portfolio. Try again later.")
	}
	ok, held := p.DeductAsset(resolved.Symbol, qty)
	if !ok {
		return chathelper.Reply(ctx, b, chatID,
			"Insufficient "+resolved.Symbol+". You have: "+FormatStock(float64(held)))
	}
	revenue := float64(qty) * price
	p.AddCurrency("VND", revenue)
	if err := SavePortfolio(ctx, s.kv, userID, p); err != nil {
		log.Error("trading_save_portfolio", "user", userID, "err", err)
		return chathelper.Reply(ctx, b, chatID, "Could not save portfolio. Try again later.")
	}
	return chathelper.Reply(ctx, b, chatID,
		"Sold "+FormatStock(float64(qty))+" "+resolved.Symbol+
			" @ "+FormatVND(price)+"\nRevenue: "+FormatVND(revenue))
}

func (s *state) handleConvert(ctx context.Context, b *bot.Bot, update *models.Update) error {
	if update.Message == nil {
		return nil
	}
	return chathelper.Reply(ctx, b, update.Message.Chat.ID,
		"Currency exchange is not available yet.\n"+s.comingSoonMessage)
}

// handleStats fetches every held ticker's current price (in parallel) and
// renders the portfolio. Read-only — no portfolio mutation, so no keylock.
func (s *state) handleStats(ctx context.Context, b *bot.Bot, update *models.Update) error {
	userID, chatID, ok := senderInfo(update)
	if !ok {
		return chathelper.Reply(ctx, b, update.Message.Chat.ID,
			"Cannot identify user — /trade_stats needs a sender.")
	}
	p, err := LoadPortfolio(ctx, s.kv, userID, s.now().UnixMilli())
	if err != nil {
		log.Error("trading_load_portfolio", "user", userID, "err", err)
		return chathelper.Reply(ctx, b, chatID, "Could not load portfolio. Try again later.")
	}

	var lines []string
	lines = append(lines, "📊 Portfolio Summary\n")
	totalValue := 0.0

	if vnd := p.Currency["VND"]; vnd > 0 {
		totalValue += vnd
		lines = append(lines, "VND: "+FormatVND(vnd))
	}

	// Filter out zero-balance assets (DeductAsset removes them, but defensive).
	type held struct {
		symbol string
		qty    int64
	}
	var heldList []held
	for sym, qty := range p.Assets {
		if qty != 0 {
			heldList = append(heldList, held{sym, qty})
		}
	}

	if len(heldList) > 0 {
		lines = append(lines, "\nStocks:")
		// Sequential price fetch (Lambda has no concurrency benefit at small N
		// and goroutines complicate test seams). For typical <10 holdings,
		// total latency is bounded by sum-of-fetches; KBS responds in <500ms.
		for _, h := range heldList {
			price, err := s.prices.FetchPrice(ctx, h.symbol)
			if err != nil {
				lines = append(lines, "  "+h.symbol+" x"+FormatStock(float64(h.qty))+" (no price)")
				continue
			}
			val := float64(h.qty) * price
			totalValue += val
			lines = append(lines, "  "+h.symbol+" x"+FormatStock(float64(h.qty))+
				" @ "+FormatVND(price)+" = "+FormatVND(val))
		}
	}

	lines = append(lines, "\nTotal value: "+FormatVND(totalValue))
	lines = append(lines, "Invested: "+FormatVND(p.Meta.Invested))
	lines = append(lines, "P&L: "+FormatPnL(totalValue, p.Meta.Invested))
	return chathelper.Reply(ctx, b, chatID, strings.Join(lines, "\n"))
}
