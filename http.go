package alltcgapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ResponseInfo is what the response says in its headers and the body does not
// carry: what the call cost, what is left of the quota, and the rate limit.
type ResponseInfo struct {
	URL       string
	Status    int
	RequestID string
	// ErrorCode equals the error's Code on API errors; empty on success.
	ErrorCode string
	// CreditsCost is X-Credits-Cost: the price of the route, written BEFORE the
	// handler runs. An error or a 304 is refunded but the header stays: what was
	// really charged shows in the next QuotaRemaining.
	CreditsCost    *int
	QuotaLimit     *int
	QuotaRemaining *int
	// QuotaReset is when a paid period renews. Empty on the trial, which does not renew.
	QuotaReset         string
	RateLimitLimit     *int
	RateLimitRemaining *int
}

func headerInt(h http.Header, name string) *int {
	raw := strings.TrimSpace(h.Get(name))
	if raw == "" {
		return nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return nil
	}
	return &n
}

func toResponseInfo(rawURL string, resp *http.Response) *ResponseInfo {
	h := resp.Header
	return &ResponseInfo{
		URL:                rawURL,
		Status:             resp.StatusCode,
		RequestID:          h.Get("X-Request-Id"),
		ErrorCode:          h.Get("X-Error-Code"),
		CreditsCost:        headerInt(h, "X-Credits-Cost"),
		QuotaLimit:         headerInt(h, "X-Quota-Limit"),
		QuotaRemaining:     headerInt(h, "X-Quota-Remaining"),
		QuotaReset:         h.Get("X-Quota-Reset"),
		RateLimitLimit:     headerInt(h, "RateLimit-Limit"),
		RateLimitRemaining: headerInt(h, "RateLimit-Remaining"),
	}
}

// DefaultBaseURL is the production host of the API.
const DefaultBaseURL = "https://api.alltcgapi.com"

const (
	defaultTimeout    = 30 * time.Second
	defaultMaxRetries = 2
	maxRetryAfter     = 60 * time.Second
)

// sleep waits for d or until ctx is done. A variable so tests can replace it.
var sleep = func(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// isRetryable: network failures, 5xx, 408 and rate-limit 429s are the only set worth retrying.
func isRetryable(err error) bool {
	var quota *QuotaExceededError
	if errors.As(err, &quota) {
		return false
	}
	var limited *RateLimitedError
	var server *ServerError
	var conn *ConnectionError
	if errors.As(err, &limited) || errors.As(err, &server) || errors.As(err, &conn) {
		return true
	}
	var api *APIError
	return errors.As(err, &api) && api.Status == http.StatusRequestTimeout
}

// random is the jitter source. A variable so tests can pin it.
var random = rand.Float64

// backoff is exponential with full jitter. The jitter is not a detail:
// without it, a thousand clients hit by the same 429 retry in the same
// millisecond and rebuild the queue they were trying to drain.
func backoff(attempt int, retryAfter *time.Duration) time.Duration {
	if retryAfter != nil {
		return min(*retryAfter, maxRetryAfter)
	}
	ceiling := min(500*time.Millisecond<<attempt, 8*time.Second)
	return time.Duration(random() * float64(ceiling))
}

type etagEntry struct {
	etag string
	body []byte
}

func (c *Client) url(path string, q url.Values) string {
	encoded := q.Encode()
	if encoded == "" {
		return c.baseURL + path
	}
	return c.baseURL + path + "?" + encoded
}

// get performs a GET with retries and decodes the body into out.
func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	return c.request(ctx, c.url(path, q), out)
}

// follow requests a URL the API already built (links.next), byte for byte.
// The cursor belongs to the sort order of the list that issued it: rebuilding
// it by hand, or reusing it on another list, is what the API rejects with
// INVALID_CURSOR.
func (c *Client) follow(ctx context.Context, absoluteURL string, out any) error {
	return c.request(ctx, absoluteURL, out)
}

func (c *Client) request(ctx context.Context, rawURL string, out any) error {
	for attempt := 0; ; attempt++ {
		err := c.attempt(ctx, rawURL, out)
		if err == nil {
			return nil
		}
		if attempt >= c.maxRetries || !isRetryable(err) || ctx.Err() != nil {
			return err
		}
		var retryAfter *time.Duration
		var limited *RateLimitedError
		if errors.As(err, &limited) && limited.HasRetryAfter {
			retryAfter = &limited.RetryAfter
		}
		if err := sleep(ctx, backoff(attempt, retryAfter)); err != nil {
			return err
		}
	}
}

func (c *Client) attempt(ctx context.Context, rawURL string, out any) error {
	attemptCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return &ConnectionError{URL: rawURL, Err: err}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if c.apiKey != "" {
		req.Header.Set("X-Api-Key", c.apiKey)
	}

	var cached *etagEntry
	if c.etags != nil {
		c.etagMu.Lock()
		if entry, ok := c.etags[rawURL]; ok {
			cached = &entry
		}
		c.etagMu.Unlock()
		if cached != nil {
			req.Header.Set("If-None-Match", cached.etag)
		}
	}

	resp, err := c.httpc.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			// The caller cancelled: hand their error back untouched.
			return ctx.Err()
		}
		if errors.Is(attemptCtx.Err(), context.DeadlineExceeded) {
			return &TimeoutError{ConnectionError: &ConnectionError{URL: rawURL, Err: err}, Timeout: c.timeout}
		}
		return &ConnectionError{URL: rawURL, Err: err}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		if errors.Is(attemptCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
			return &TimeoutError{ConnectionError: &ConnectionError{URL: rawURL, Err: err}, Timeout: c.timeout}
		}
		return &ConnectionError{URL: rawURL, Err: err}
	}

	info := toResponseInfo(rawURL, resp)
	c.last.Store(info)
	if c.onResponse != nil {
		c.onResponse(*info)
	}

	// 304: the body is empty by definition, the answer is the cached one.
	if resp.StatusCode == http.StatusNotModified && cached != nil {
		return decode(cached.body, out)
	}

	if resp.StatusCode >= 300 {
		var retryAfter *time.Duration
		if header := strings.TrimSpace(resp.Header.Get("Retry-After")); header != "" {
			if seconds, err := strconv.Atoi(header); err == nil {
				d := time.Duration(seconds) * time.Second
				retryAfter = &d
			}
		}
		return toAPIError(resp.StatusCode, readErrorBody(resp.StatusCode, raw), retryAfter)
	}

	if err := decode(raw, out); err != nil {
		return err
	}
	if c.etags != nil {
		if etag := resp.Header.Get("ETag"); etag != "" {
			c.etagMu.Lock()
			c.etags[rawURL] = etagEntry{etag: etag, body: raw}
			c.etagMu.Unlock()
		}
	}
	return nil
}

func decode(raw []byte, out any) error {
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// readErrorBody accepts the API envelope, and builds an error from the status
// alone when the body is something else (an HTML page from a proxy).
func readErrorBody(status int, raw []byte) errorBody {
	var parsed struct {
		Error *errorBody `json:"error"`
	}
	if err := json.Unmarshal(raw, &parsed); err == nil && parsed.Error != nil && parsed.Error.Code != "" {
		return *parsed.Error
	}
	return fallbackErrorBody(status)
}
