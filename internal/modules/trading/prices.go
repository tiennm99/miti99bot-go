package trading

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// kbsDefaultURL is the KBS public stock data endpoint base.
const kbsDefaultURL = "https://kbbuddywts.kbsec.com.vn/iis-server/investment/stocks"

// kbsLookbackDays widens the requested window to absorb weekends and Vietnam
// market holidays — KBS returns the latest bar within the window in [0].
const kbsLookbackDays = 14

// kbsHTTPTimeout caps the price fetch. KBS is generally fast; 10s leaves
// headroom for TLS + DNS on a Lambda cold start.
const kbsHTTPTimeout = 10 * time.Second

// PriceClient is the KBS price fetcher. Zero value uses the default URL +
// `&{Timeout: kbsHTTPTimeout}` HTTP client; tests inject HTTP + URL.
type PriceClient struct {
	HTTP *http.Client
	URL  string
}

func (c *PriceClient) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: kbsHTTPTimeout}
}

func (c *PriceClient) baseURL() string {
	if c.URL != "" {
		return c.URL
	}
	return kbsDefaultURL
}

// kbsResponse is the slice of the KBS payload we care about. We intentionally
// don't model the full response (open/high/low/volume) — only the latest
// close. The struct still names them so Json doesn't error on unknown fields
// (Go's json decoder ignores them by default).
type kbsResponse struct {
	DataDay []kbsBar `json:"data_day"`
}

type kbsBar struct {
	C float64 `json:"c"` // close, already in VND, unscaled
}

// kbsFormatDate formats t as "DD-MM-YYYY" — KBS's expected query date shape.
func kbsFormatDate(t time.Time) string {
	t = t.UTC()
	return fmt.Sprintf("%02d-%02d-%04d", t.Day(), int(t.Month()), t.Year())
}

// FetchPrice returns the latest VND close for ticker, or ErrNoPrice if KBS
// has no data (unknown ticker, suspended, holiday-only window). Network /
// decode errors are returned wrapped.
func (c *PriceClient) FetchPrice(ctx context.Context, ticker string) (float64, error) {
	if ticker == "" {
		return 0, errors.New("trading: ticker is empty")
	}
	now := time.Now().UTC()
	edate := kbsFormatDate(now)
	sdate := kbsFormatDate(now.Add(-time.Duration(kbsLookbackDays) * 24 * time.Hour))

	endpoint := c.baseURL() + "/" + url.PathEscape(ticker) + "/data_day"
	q := url.Values{}
	q.Set("sdate", sdate)
	q.Set("edate", edate)
	full := endpoint + "?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, full, nil)
	if err != nil {
		return 0, fmt.Errorf("trading: build KBS request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (miti99bot-go)")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return 0, fmt.Errorf("trading: KBS request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, ErrNoPrice
	}

	var body kbsResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, fmt.Errorf("trading: KBS decode: %w", err)
	}
	if len(body.DataDay) == 0 {
		return 0, ErrNoPrice
	}
	close := body.DataDay[0].C
	if close <= 0 {
		return 0, ErrNoPrice
	}
	return close, nil
}

// ErrNoPrice means KBS returned no usable price for the ticker — either the
// symbol is unknown, the market hasn't traded recently, or the data was
// invalid. Used by symbol resolution to detect "is this a real ticker".
var ErrNoPrice = errors.New("trading: no price available")
