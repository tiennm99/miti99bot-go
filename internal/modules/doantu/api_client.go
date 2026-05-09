// Package doantu ports the JS Vietnamese-semantle module to Go. It uses the
// hosted phow2sim PhoW2V word2vec API (https://phow2sim.sg.miti99.com) for
// target picking + cosine similarity, NOT Gemini embeddings. Rationale:
//
//  1. text-embedding-004 was not trained for Vietnamese semantic relatedness;
//     phow2sim is a domain-trained model.
//  2. phow2sim already owns the vocabulary — no need to maintain a Vietnamese
//     wordlist alongside it.
//  3. JS parity: the upstream service is the same one the JS bot has been
//     using; switching to embeddings would diverge behavior, not preserve it.
//
// Phase 07 plan suggested embedding both modules; this is a documented
// deviation. Update phase-07 plan + plan.md when reviewing.
package doantu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultTimeout = 5 * time.Second
	userAgent      = "miti99bot-go/doantu"
)

// UpstreamError is returned for every transport / decode failure. status is
// 0 for non-HTTP failures (timeout, DNS).
type UpstreamError struct {
	Status int
	Msg    string
	Body   string
}

func (e *UpstreamError) Error() string {
	if e.Status > 0 {
		return fmt.Sprintf("phow2sim HTTP %d: %s", e.Status, e.Msg)
	}
	return "phow2sim: " + e.Msg
}

// SimResp mirrors the JS api-client similarity() shape.
type SimResp struct {
	A           string   `json:"a"`
	B           string   `json:"b"`
	CanonicalA  *string  `json:"canonical_a"`
	CanonicalB  *string  `json:"canonical_b"`
	InVocabA    bool     `json:"in_vocab_a"`
	InVocabB    bool     `json:"in_vocab_b"`
	Similarity  *float64 `json:"similarity"`
}

// RandomResp shape from /random.
type RandomResp struct {
	Word string `json:"word"`
	Rank *int   `json:"rank,omitempty"`
}

// Neighbor item in /neighbors response.
type Neighbor struct {
	Word       string  `json:"word"`
	Similarity float64 `json:"similarity"`
}

// NeighborsResp from /neighbors.
type NeighborsResp struct {
	Word      string     `json:"word"`
	Canonical *string    `json:"canonical"`
	InVocab   bool       `json:"in_vocab"`
	Neighbors []Neighbor `json:"neighbors"`
}

// Client is the phow2sim HTTP client. Zero value is unusable — call
// NewClient. Safe for concurrent use; net/http.Client is goroutine-safe.
type Client struct {
	base string
	hc   *http.Client
}

// NewClient builds a client against the supplied base URL (no trailing
// slash needed; we strip it). timeout=0 → defaultTimeout.
func NewClient(base string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Client{
		base: strings.TrimRight(base, "/"),
		hc:   &http.Client{Timeout: timeout},
	}
}

func (c *Client) get(ctx context.Context, path string, params url.Values, dst any) error {
	full := c.base + path
	if len(params) > 0 {
		full += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, full, nil)
	if err != nil {
		return &UpstreamError{Msg: "build request: " + err.Error()}
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return &UpstreamError{Msg: "fetch failed: " + err.Error()}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB cap
	if err != nil {
		return &UpstreamError{Status: resp.StatusCode, Msg: "read body: " + err.Error()}
	}
	if resp.StatusCode != http.StatusOK {
		return &UpstreamError{Status: resp.StatusCode, Msg: "non-200", Body: truncate(string(body), 500)}
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return &UpstreamError{Status: resp.StatusCode, Msg: "decode: " + err.Error(), Body: truncate(string(body), 200)}
	}
	return nil
}

// RandomWord picks a target word with optional filters (e.g. min_rank/max_rank).
func (c *Client) RandomWord(ctx context.Context, filters map[string]string) (*RandomResp, error) {
	q := url.Values{}
	for k, v := range filters {
		q.Set(k, v)
	}
	var r RandomResp
	if err := c.get(ctx, "/random", q, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// Similarity returns target↔guess cosine + canonical forms + vocab flags.
func (c *Client) Similarity(ctx context.Context, a, b string) (*SimResp, error) {
	q := url.Values{}
	q.Set("a", a)
	q.Set("b", b)
	var r SimResp
	if err := c.get(ctx, "/similarity", q, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// Neighbors returns the top-N closest words to `word`. JS-parity default 100.
func (c *Client) Neighbors(ctx context.Context, word string, topn int) (*NeighborsResp, error) {
	if topn <= 0 {
		topn = 100
	}
	q := url.Values{}
	q.Set("word", word)
	q.Set("topn", strconv.Itoa(topn))
	var r NeighborsResp
	if err := c.get(ctx, "/neighbors", q, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// SimAPI is the small interface handlers consume — lets tests pass a fake.
type SimAPI interface {
	RandomWord(ctx context.Context, filters map[string]string) (*RandomResp, error)
	Similarity(ctx context.Context, a, b string) (*SimResp, error)
	Neighbors(ctx context.Context, word string, topn int) (*NeighborsResp, error)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
