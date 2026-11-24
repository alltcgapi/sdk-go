package alltcgapi

import (
	"context"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Client is the entry point. Resources mirror the API paths (client.Cards.Get,
// client.Sets.Cards) so that going from the documentation to the code needs
// no conversion table. Every list method returns a *Page that walks the
// collection by following links.next; methods that read one object or one
// array drop the data envelope and return it.
//
//	client := alltcgapi.New(alltcgapi.WithAPIKey(os.Getenv("ALLTCG_API_KEY")))
//	page, err := client.Cards.List(ctx, &alltcgapi.CardListParams{Game: alltcgapi.GameYuGiOh, Number: "MAMS-EN001"})
type Client struct {
	Games     *GamesService
	Sets      *SetsService
	Cards     *CardsService
	Sealed    *SealedService
	Prices    *PricesService
	Reference *ReferenceService

	baseURL    string
	apiKey     string
	timeout    time.Duration
	maxRetries int
	httpc      *http.Client
	userAgent  string
	etags      map[string]etagEntry
	etagMu     sync.Mutex
	onResponse func(ResponseInfo)
	last       atomic.Pointer[ResponseInfo]
}

// Option configures a Client.
type Option func(*Client)

// WithAPIKey sets the key. Without it, ALLTCG_API_KEY is read from the environment.
func WithAPIKey(key string) Option { return func(c *Client) { c.apiKey = key } }

// WithBaseURL points the client somewhere else, e.g. a test server.
func WithBaseURL(baseURL string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(baseURL, "/") }
}

// WithHTTPClient injects the transport. Its Timeout is not used: the SDK
// applies its own per-attempt timeout through the context.
func WithHTTPClient(httpc *http.Client) Option { return func(c *Client) { c.httpc = httpc } }

// WithTimeout sets the timeout of a single attempt, not of the whole call.
func WithTimeout(d time.Duration) Option { return func(c *Client) { c.timeout = d } }

// WithMaxRetries sets how many times a GET is retried after the first attempt. 0 disables.
func WithMaxRetries(n int) Option { return func(c *Client) { c.maxRetries = n } }

// WithETagCache remembers ETags and sends If-None-Match. Worth it: a 304 has
// no body and costs no credits, so a mirror that re-syncs often pays only for
// the pages that changed. The cache is in memory and per client, on purpose.
func WithETagCache() Option { return func(c *Client) { c.etags = map[string]etagEntry{} } }

// WithUserAgent replaces the default User-Agent.
func WithUserAgent(ua string) Option { return func(c *Client) { c.userAgent = ua } }

// WithOnResponse registers a callback for every response received, error
// responses included: the place for a credit counter.
func WithOnResponse(fn func(ResponseInfo)) Option { return func(c *Client) { c.onResponse = fn } }

// New builds a client. The transport is the standard library's; there are no
// dependencies to carry.
func New(opts ...Option) *Client {
	c := &Client{
		baseURL:    DefaultBaseURL,
		apiKey:     os.Getenv("ALLTCG_API_KEY"),
		timeout:    defaultTimeout,
		maxRetries: defaultMaxRetries,
		httpc:      &http.Client{},
		userAgent:  defaultUserAgent,
	}
	for _, opt := range opts {
		opt(c)
	}
	c.Games = &GamesService{c}
	c.Sets = &SetsService{c}
	c.Cards = &CardsService{c}
	c.Sealed = &SealedService{c}
	c.Prices = &PricesService{c}
	c.Reference = &ReferenceService{client: c}
	return c
}

// LastResponse returns the headers of the last response this client received,
// or nil before the first one. With concurrent calls it is the last to arrive:
// to count them all, use WithOnResponse.
func (c *Client) LastResponse() *ResponseInfo { return c.last.Load() }

// Plans returns the price list (/v1/billing/plans). Public: no key, no credits.
func (c *Client) Plans(ctx context.Context) ([]Plan, error) {
	var out envelope[[]Plan]
	if err := c.get(ctx, "/v1/billing/plans", nil, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// Status says when the last refresh finished, per lane. Public.
func (c *Client) Status(ctx context.Context) (*CatalogStatus, error) {
	var out envelope[CatalogStatus]
	if err := c.get(ctx, "/v1/status", nil, &out); err != nil {
		return nil, err
	}
	return &out.Data, nil
}

// Health is the liveness probe: process alive and database reachable. Public.
// A degraded service answers 503, which becomes *ServerError after the retries.
func (c *Client) Health(ctx context.Context) (*Health, error) {
	var out Health
	if err := c.get(ctx, "/v1/health", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
