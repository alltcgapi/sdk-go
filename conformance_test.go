package alltcgapi

// One test per item of packages/CONFORMANCE.md, named after it. The
// TypeScript and Python suites have the same. Every test is offline: a
// RoundTripper answers from the fixtures, so the tests check exactly what
// travels on the wire.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── helpers ─────────────────────────────────────────────────────────────────

type fixtureDoc struct {
	Status   int               `json:"status"`
	Headers  map[string]string `json:"headers"`
	Body     json.RawMessage   `json:"body"`
	BodyText *string           `json:"body_text"`
}

// fixturesDir: in the public repo sync-public.sh copies the fixtures into
// testdata/fixtures; in the monorepo they live once, next to this package.
func fixturesDir(t *testing.T) string {
	t.Helper()
	for _, dir := range []string{filepath.Join("testdata", "fixtures"), filepath.Join("..", "fixtures")} {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}
	t.Fatal("fixtures not found in testdata/fixtures or ../fixtures")
	return ""
}

func loadDoc(t *testing.T, name string) fixtureDoc {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixturesDir(t), name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc fixtureDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return doc
}

func (d fixtureDoc) response(req *http.Request) *http.Response {
	body := []byte(d.Body)
	if d.BodyText != nil {
		body = []byte(*d.BodyText)
	}
	header := http.Header{}
	for k, v := range d.Headers {
		header.Set(k, v)
	}
	return &http.Response{
		StatusCode: d.Status,
		Status:     fmt.Sprintf("%d %s", d.Status, http.StatusText(d.Status)),
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(string(body))),
		Request:    req,
	}
}

func jsonResponse(req *http.Request, status int, body any, headers map[string]string) *http.Response {
	raw, _ := json.Marshal(body)
	return fixtureDoc{Status: status, Headers: headers, Body: raw}.response(req)
}

type answer func(*http.Request) (*http.Response, error)

// recorder is a RoundTripper that records every request and answers from a
// queue; the last answer repeats.
type recorder struct {
	t        *testing.T
	mu       sync.Mutex
	queue    []answer
	requests []*http.Request
}

func newRecorder(t *testing.T, answers ...any) *recorder {
	r := &recorder{t: t}
	for _, a := range answers {
		switch v := a.(type) {
		case string:
			doc := loadDoc(t, v)
			r.queue = append(r.queue, func(req *http.Request) (*http.Response, error) { return doc.response(req), nil })
		case answer:
			r.queue = append(r.queue, v)
		case func(*http.Request) (*http.Response, error):
			r.queue = append(r.queue, v)
		default:
			t.Fatalf("unsupported answer %T", a)
		}
	}
	return r
}

func (r *recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.requests = append(r.requests, req)
	if len(r.queue) == 0 {
		r.mu.Unlock()
		r.t.Fatalf("unexpected request %s", req.URL)
	}
	next := r.queue[0]
	if len(r.queue) > 1 {
		r.queue = r.queue[1:]
	}
	r.mu.Unlock()
	return next(req)
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

func (r *recorder) urls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.requests))
	for i, req := range r.requests {
		out[i] = req.URL.String()
	}
	return out
}

// paths are the requests made so far, each as path and query.
func (r *recorder) paths() []string {
	urls := r.urls()
	out := make([]string, len(urls))
	for i, u := range urls {
		out[i] = pathOf(u)
	}
	return out
}

func okBody(body any, headers map[string]string) answer {
	return func(req *http.Request) (*http.Response, error) { return jsonResponse(req, 200, body, headers), nil }
}

func emptyList() answer {
	return okBody(map[string]any{
		"data": []any{}, "meta": map[string]any{"limit": 50, "count": 0, "has_more": false}, "links": map[string]any{"next": nil},
	}, nil)
}

func testClient(rt http.RoundTripper, opts ...Option) *Client {
	base := []Option{WithAPIKey("test"), WithMaxRetries(0), WithHTTPClient(&http.Client{Transport: rt})}
	return New(append(base, opts...)...)
}

// noSleep replaces the backoff sleep with a recorder of the waits.
func noSleep(t *testing.T) *[]time.Duration {
	t.Helper()
	waits := &[]time.Duration{}
	var mu sync.Mutex
	previous := sleep
	sleep = func(ctx context.Context, d time.Duration) error {
		mu.Lock()
		*waits = append(*waits, d)
		mu.Unlock()
		return ctx.Err()
	}
	t.Cleanup(func() { sleep = previous })
	return waits
}

func pathOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if u.RawQuery == "" {
		return u.EscapedPath()
	}
	return u.EscapedPath() + "?" + u.RawQuery
}

func ptr[T any](v T) *T { return &v }

// wholeBody fails unless v, written back as JSON, says what the body of the
// fixture says. A method that returns the body whole decodes it into typed
// structs: a key the types did not carry would go missing here.
func wholeBody(t *testing.T, fixture string, v any) {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	body := loadDoc(t, fixture).Body
	var got, want any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s: the body did not come back whole:\n got %s\nwant %s", fixture, raw, body)
	}
}

// ── conformance items ───────────────────────────────────────────────────────

func TestParamsEncoding(t *testing.T) {
	ctx := context.Background()
	rec := newRecorder(t, okBody(map[string]any{"data": map[string]any{}}, nil))
	client := testClient(rec)
	_, _ = client.Cards.Get(ctx, "a/b c?d")
	_, _ = client.Cards.List(ctx, &CardListParams{Game: GameYuGiOh, Number: "MAMS-EN001", Name: ""})
	_, _ = client.Cards.List(ctx, &CardListParams{Name: "Blue-Eyes White"})
	_, _ = client.Cards.List(ctx, nil)
	got := make([]string, 0, 4)
	for _, u := range rec.urls() {
		got = append(got, pathOf(u))
	}
	want := []string{
		"/v1/cards/a%2Fb%20c%3Fd",
		"/v1/cards?game=yugioh&number=MAMS-EN001",
		"/v1/cards?name=Blue-Eyes+White",
		"/v1/cards",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("paths:\n got %q\nwant %q", got, want)
	}
}

func TestEnvKeyFallback(t *testing.T) {
	ctx := context.Background()
	rec := newRecorder(t, okBody(map[string]any{"data": []any{}}, nil))
	httpc := WithHTTPClient(&http.Client{Transport: rec})

	t.Setenv("ALLTCG_API_KEY", "from-env")
	_, _ = New(httpc).Games.List(ctx)
	t.Setenv("ALLTCG_API_KEY", "")
	_, _ = New(httpc).Games.List(ctx)
	_, _ = New(httpc, WithAPIKey("explicit")).Games.List(ctx)

	if got := rec.requests[0].Header.Get("X-Api-Key"); got != "from-env" {
		t.Fatalf("env key: %q", got)
	}
	if _, present := rec.requests[1].Header["X-Api-Key"]; present {
		t.Fatal("an empty env key must not send the header")
	}
	if got := rec.requests[2].Header.Get("X-Api-Key"); got != "explicit" {
		t.Fatalf("explicit key: %q", got)
	}
}

func TestHeadersSent(t *testing.T) {
	ctx := context.Background()
	rec := newRecorder(t, okBody(map[string]any{"data": []any{}}, nil))
	_, _ = testClient(rec).Games.List(ctx)
	_, _ = testClient(rec, WithUserAgent("my-bot/1.0")).Games.List(ctx)
	h := rec.requests[0].Header
	if h.Get("Accept") != "application/json" || h.Get("X-Api-Key") != "test" {
		t.Fatalf("headers: %v", h)
	}
	if got := h.Get("User-Agent"); got != "alltcgapi-go/"+Version {
		t.Fatalf("user-agent: %q", got)
	}
	if got := rec.requests[1].Header.Get("User-Agent"); got != "my-bot/1.0" {
		t.Fatalf("custom user-agent: %q", got)
	}
	if !strings.HasPrefix(rec.requests[0].URL.String(), DefaultBaseURL+"/") {
		t.Fatalf("base url: %s", rec.requests[0].URL)
	}
}

func TestResponseInfo(t *testing.T) {
	ctx := context.Background()
	rec := newRecorder(t, "games", okBody(map[string]any{"data": []any{}}, map[string]string{
		"x-credits-cost": "abc", "x-quota-reset": "2026-12-01T00:00:00Z",
	}))
	client := testClient(rec)
	if client.LastResponse() != nil {
		t.Fatal("LastResponse before the first call must be nil")
	}
	if _, err := client.Games.List(ctx); err != nil {
		t.Fatal(err)
	}
	want := ResponseInfo{
		URL:                DefaultBaseURL + "/v1/games",
		Status:             200,
		RequestID:          "00000000000000000000000000000001",
		CreditsCost:        ptr(1),
		QuotaLimit:         ptr(800),
		QuotaRemaining:     ptr(799),
		RateLimitLimit:     ptr(5),
		RateLimitRemaining: ptr(4),
	}
	if got := *client.LastResponse(); !reflect.DeepEqual(got, want) {
		t.Fatalf("info:\n got %+v\nwant %+v", got, want)
	}
	_, _ = client.Games.List(ctx)
	info := client.LastResponse()
	if info.CreditsCost != nil || info.QuotaReset != "2026-12-01T00:00:00Z" || info.RequestID != "" {
		t.Fatalf("second info: %+v", info)
	}
}

func TestOnResponseCalledOnErrors(t *testing.T) {
	var seen []ResponseInfo
	client := testClient(newRecorder(t, "404-card-not-found"), WithOnResponse(func(info ResponseInfo) { seen = append(seen, info) }))
	_, err := client.Cards.Get(context.Background(), "00000000-0000-4000-8000-000000000000")
	var nf *NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("want NotFoundError, got %T %v", err, err)
	}
	if len(seen) != 1 || seen[0].Status != 404 || seen[0].ErrorCode != "CARD_NOT_FOUND" {
		t.Fatalf("seen: %+v", seen)
	}
	if client.LastResponse().ErrorCode != "CARD_NOT_FOUND" {
		t.Fatal("LastResponse must be set on errors too")
	}
}

func TestErrorMappingTable(t *testing.T) {
	cases := []struct {
		fixture string
		code    string
		is      func(error) bool
	}{
		{"429-quota-exceeded", "QUOTA_EXCEEDED", func(e error) bool { var x *QuotaExceededError; return errors.As(e, &x) }},
		{"403-trial-expired", "TRIAL_EXPIRED", func(e error) bool { var x *TrialExpiredError; return errors.As(e, &x) }},
		{"429-rate-limited", "RATE_LIMITED", func(e error) bool { var x *RateLimitedError; return errors.As(e, &x) }},
		{"429-concurrency-limit", "CONCURRENCY_LIMIT", func(e error) bool { var x *RateLimitedError; return errors.As(e, &x) }},
		{"401-missing-key", "MISSING_API_KEY", func(e error) bool { var x *AuthenticationError; return errors.As(e, &x) }},
		{"404-card-not-found", "CARD_NOT_FOUND", func(e error) bool { var x *NotFoundError; return errors.As(e, &x) }},
		{"400-invalid-parameter", "INVALID_PARAMETER", func(e error) bool { var x *InvalidRequestError; return errors.As(e, &x) }},
		{"400-limit-exceeded", "LIMIT_EXCEEDED", func(e error) bool { var x *InvalidRequestError; return errors.As(e, &x) }},
		{"503-meter-unavailable", "METER_UNAVAILABLE", func(e error) bool { var x *ServerError; return errors.As(e, &x) }},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			doc := loadDoc(t, tc.fixture)
			var body struct {
				Error struct {
					Message   string `json:"message"`
					RequestID string `json:"request_id"`
				} `json:"error"`
			}
			if err := json.Unmarshal(doc.Body, &body); err != nil {
				t.Fatal(err)
			}
			_, err := testClient(newRecorder(t, tc.fixture)).Games.List(context.Background())
			if !tc.is(err) {
				t.Fatalf("wrong class %T", err)
			}
			var api *APIError
			if !errors.As(err, &api) {
				t.Fatalf("must unwrap to *APIError: %T", err)
			}
			if api.Code != tc.code || api.Status != doc.Status || api.RequestID != body.Error.RequestID {
				t.Fatalf("api error: %+v", api)
			}
			if err.Error() != tc.code+": "+body.Error.Message {
				t.Fatalf("message: %q", err.Error())
			}
		})
	}

	// The code decides before the status.
	var denied *PermissionDeniedError
	if err := toAPIError(403, errorBody{Code: "TRIAL_EXPIRED"}, nil); !errors.As(err, &denied) {
		t.Fatal("TrialExpiredError must unwrap to *PermissionDeniedError")
	}
	var trial *TrialExpiredError
	if err := toAPIError(403, errorBody{Code: "INSUFFICIENT_SCOPE"}, nil); errors.As(err, &trial) || !errors.As(err, &denied) {
		t.Fatal("a plain 403 is PermissionDenied, not TrialExpired")
	}
	var nf *NotFoundError
	if err := toAPIError(400, errorBody{Code: "SET_NOT_FOUND"}, nil); !errors.As(err, &nf) {
		t.Fatal("*_NOT_FOUND maps to NotFoundError whatever the status")
	}
	var limited *RateLimitedError
	if err := toAPIError(429, errorBody{Code: "RATE_LIMITED"}, ptr(7*time.Second)); !errors.As(err, &limited) || limited.RetryAfter != 7*time.Second || !limited.HasRetryAfter {
		t.Fatal("RateLimitedError must carry Retry-After")
	}
	if err := toAPIError(302, errorBody{Code: "HTTP_302"}, nil); reflect.TypeOf(err) != reflect.TypeOf(&APIError{}) {
		t.Fatalf("a non-4xx/5xx status stays a plain *APIError, got %T", err)
	}
}

func TestErrorBodyFallback(t *testing.T) {
	_, err := testClient(newRecorder(t, "502-html")).Games.List(context.Background())
	var server *ServerError
	if !errors.As(err, &server) {
		t.Fatalf("want ServerError, got %T", err)
	}
	if server.Code != "HTTP_502" || server.Status != 502 || err.Error() != "HTTP_502: Bad Gateway" || server.RequestID != "" {
		t.Fatalf("fallback: %+v", server.APIError)
	}
}

func TestParamAccessor(t *testing.T) {
	_, err := testClient(newRecorder(t, "400-invalid-parameter")).Sets.List(context.Background(), &SetListParams{Game: "one-piece"})
	var invalid *InvalidRequestError
	if !errors.As(err, &invalid) || invalid.Param() != "game" {
		t.Fatalf("param: %v", err)
	}
	allowed, _ := invalid.Details["allowed"].([]any)
	if len(allowed) != len(GameIDs) {
		t.Fatalf("details.allowed: %v", invalid.Details["allowed"])
	}
	for i, id := range GameIDs {
		if allowed[i] != id {
			t.Fatalf("allowed[%d] = %v, want %s", i, allowed[i], id)
		}
	}
	if (&APIError{Details: map[string]any{"max": 250.0}}).Param() != "" {
		t.Fatal("no param → empty")
	}
	if (&APIError{Details: map[string]any{"param": 7.0}}).Param() != "" {
		t.Fatal("a non-string param → empty")
	}
}

func TestRetryPolicy(t *testing.T) {
	ctx := context.Background()
	waits := noSleep(t)

	// 429 with Retry-After, then 200: one wait of exactly Retry-After.
	rec := newRecorder(t, "429-rate-limited", "games")
	games, err := testClient(rec, WithMaxRetries(2)).Games.List(ctx)
	if err != nil || len(games) != len(GameIDs) || rec.count() != 2 {
		t.Fatalf("rate limited: %v, %d games, %d requests", err, len(games), rec.count())
	}
	if !reflect.DeepEqual(*waits, []time.Duration{time.Second}) {
		t.Fatalf("waits: %v", *waits)
	}

	rec = newRecorder(t, "429-concurrency-limit", "games")
	if _, err := testClient(rec, WithMaxRetries(2)).Games.List(ctx); err != nil || rec.count() != 2 {
		t.Fatalf("concurrency: %v, %d", err, rec.count())
	}

	// 5xx every time: maxRetries + 1 attempts, then the error surfaces.
	rec = newRecorder(t, "502-html")
	var server *ServerError
	if _, err := testClient(rec, WithMaxRetries(2)).Games.List(ctx); !errors.As(err, &server) || rec.count() != 3 {
		t.Fatalf("5xx: %v, %d", err, rec.count())
	}

	rec = newRecorder(t, "503-meter-unavailable", "games")
	if _, err := testClient(rec, WithMaxRetries(2)).Games.List(ctx); err != nil || rec.count() != 2 {
		t.Fatalf("meter: %v, %d", err, rec.count())
	}

	// Quota is a 429 with a Retry-After of hours: never retried.
	*waits = nil
	rec = newRecorder(t, "429-quota-exceeded")
	var quota *QuotaExceededError
	if _, err := testClient(rec, WithMaxRetries(2)).Games.List(ctx); !errors.As(err, &quota) || rec.count() != 1 || len(*waits) != 0 {
		t.Fatalf("quota: %v, %d requests, waits %v", err, rec.count(), *waits)
	}

	// Other 4xx are not retried either.
	for _, name := range []string{"404-card-not-found", "400-invalid-parameter", "401-missing-key"} {
		rec = newRecorder(t, name)
		if _, err := testClient(rec, WithMaxRetries(2)).Games.List(ctx); err == nil || rec.count() != 1 {
			t.Fatalf("%s: %v, %d", name, err, rec.count())
		}
	}

	// Connection failures and 408 are.
	calls := 0
	flaky := answer(func(req *http.Request) (*http.Response, error) {
		calls++
		switch calls {
		case 1:
			return nil, errors.New("connection reset")
		case 2:
			return jsonResponse(req, 408, map[string]any{"error": map[string]any{"code": "REQUEST_TIMEOUT", "message": "slow"}}, nil), nil
		}
		return jsonResponse(req, 200, map[string]any{"data": []any{}}, nil), nil
	})
	if _, err := testClient(newRecorder(t, flaky), WithMaxRetries(2)).Games.List(ctx); err != nil || calls != 3 {
		t.Fatalf("flaky: %v, %d", err, calls)
	}

	// WithMaxRetries(0) means a single attempt.
	rec = newRecorder(t, "429-rate-limited")
	if _, err := testClient(rec, WithMaxRetries(0)).Games.List(ctx); err == nil || rec.count() != 1 {
		t.Fatalf("no retries: %v, %d", err, rec.count())
	}

	// Backoff: Retry-After capped at 60 s, otherwise full jitter up to 8 s.
	if backoff(0, ptr(5*time.Second)) != 5*time.Second || backoff(3, ptr(time.Hour)) != 60*time.Second {
		t.Fatal("Retry-After backoff")
	}
	previous := random
	t.Cleanup(func() { random = previous })
	random = func() float64 { return 1 }
	for attempt, want := range []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 8 * time.Second} {
		if got := backoff(attempt, nil); got != want {
			t.Fatalf("backoff(%d) = %s, want %s", attempt, got, want)
		}
	}
	random = func() float64 { return 0 }
	if backoff(2, nil) != 0 {
		t.Fatal("full jitter goes down to zero")
	}
}

func TestTrialExpiredNotRetried(t *testing.T) {
	waits := noSleep(t)
	rec := newRecorder(t, "403-trial-expired")
	client := testClient(rec, WithMaxRetries(5))
	_, err := client.Prices.Card(context.Background(), "7a3d9e2b-5c1f-4b8a-9e6d-3f2a1c8b7e94", nil)
	var trial *TrialExpiredError
	if !errors.As(err, &trial) {
		t.Fatalf("want TrialExpiredError, got %T", err)
	}
	var denied *PermissionDeniedError
	if !errors.As(err, &denied) {
		t.Fatal("TrialExpiredError must be a PermissionDeniedError")
	}
	if trial.Code != "TRIAL_EXPIRED" || trial.RequestID != "00000000000000000000000000000017" || trial.Details["plans"] != "/v1/billing/plans" {
		t.Fatalf("trial: %+v", trial.APIError)
	}
	if rec.count() != 1 || len(*waits) != 0 {
		t.Fatalf("retried: %d requests, waits %v", rec.count(), *waits)
	}
	if q := client.LastResponse().QuotaRemaining; q == nil || *q != 0 {
		t.Fatalf("quota remaining: %v", q)
	}
}

func TestPaginationFollowsLinksNext(t *testing.T) {
	ctx := context.Background()
	var first Collection[CardSet]
	if err := json.Unmarshal(loadDoc(t, "sets-page-1").Body, &first); err != nil {
		t.Fatal(err)
	}
	rec := newRecorder(t, "sets-page-1", "sets-page-2")
	page, err := testClient(rec).Sets.List(ctx, &SetListParams{Game: GameOnePiece, PageParams: PageParams{Limit: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if !page.HasMore() || page.NextURL != *first.Links.Next {
		t.Fatalf("first page: %+v", page)
	}
	if page.Meta.Limit != 2 || page.Meta.Count != 2 || !page.Meta.HasMore || page.Meta.TotalCount != nil {
		t.Fatalf("meta: %+v", page.Meta)
	}
	second, err := page.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rec.urls()[1] != *first.Links.Next {
		t.Fatalf("followed %s, want %s", rec.urls()[1], *first.Links.Next)
	}
	if second.HasMore() || second.NextURL != "" {
		t.Fatal("last page must have no next")
	}
	if last, err := second.Next(ctx); last != nil || err != nil {
		t.Fatal("Next after the last page is (nil, nil)")
	}
	if rec.count() != 2 {
		t.Fatalf("requests: %d", rec.count())
	}

	var cardsFirst Collection[Card]
	if err := json.Unmarshal(loadDoc(t, "set-cards-page-1").Body, &cardsFirst); err != nil {
		t.Fatal(err)
	}
	walk := newRecorder(t, "set-cards-page-1", "set-cards-page-2")
	cards, err := testClient(walk).Sets.Cards(ctx, "OP01", &SetCardsParams{PageParams: PageParams{Limit: 2}})
	if err != nil {
		t.Fatal(err)
	}
	var numbers []string
	for card, err := range cards.All(ctx) {
		if err != nil {
			t.Fatal(err)
		}
		numbers = append(numbers, card.Number)
	}
	if !reflect.DeepEqual(numbers, []string{"OP01-001", "OP01-002", "OP01-003"}) {
		t.Fatalf("numbers: %v", numbers)
	}
	want := []string{DefaultBaseURL + "/v1/sets/OP01/cards?limit=2", *cardsFirst.Links.Next}
	if !reflect.DeepEqual(walk.urls(), want) {
		t.Fatalf("urls:\n got %v\nwant %v", walk.urls(), want)
	}
}

func TestToListMax(t *testing.T) {
	ctx := context.Background()
	params := &SetCardsParams{PageParams: PageParams{Limit: 2}}

	rec := newRecorder(t, "set-cards-page-1", "set-cards-page-2")
	page, _ := testClient(rec).Sets.Cards(ctx, "OP01", params)
	two, err := page.Collect(ctx, 2)
	if err != nil || len(two) != 2 || two[1].Number != "OP01-002" || rec.count() != 1 {
		t.Fatalf("collect 2: %v, %d items, %d requests", err, len(two), rec.count())
	}

	rec = newRecorder(t, "set-cards-page-1", "set-cards-page-2")
	page, _ = testClient(rec).Sets.Cards(ctx, "OP01", params)
	all, err := page.Collect(ctx, 100)
	if err != nil || len(all) != 3 || rec.count() != 2 {
		t.Fatalf("collect 100: %v, %d items, %d requests", err, len(all), rec.count())
	}

	if none, _ := page.Collect(ctx, 0); len(none) != 0 {
		t.Fatal("collect 0 is empty")
	}
}

func TestEtag304(t *testing.T) {
	ctx := context.Background()
	params := &SetListParams{Game: GameOnePiece, PageParams: PageParams{Limit: 2}}
	rec := newRecorder(t, "sets-page-1", "304", "sets-page-1")
	client := testClient(rec, WithETagCache())
	one, err := client.Sets.List(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	two, err := client.Sets.List(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	if rec.requests[0].Header.Get("If-None-Match") != "" {
		t.Fatal("first request must not send If-None-Match")
	}
	if got := rec.requests[1].Header.Get("If-None-Match"); got != `"s3ts-1"` {
		t.Fatalf("If-None-Match: %q", got)
	}
	if !reflect.DeepEqual(one.Data, two.Data) || client.LastResponse().Status != 304 {
		t.Fatal("a 304 must return the cached body")
	}
	_, _ = client.Sets.List(ctx, &SetListParams{Game: GameOnePiece, PageParams: PageParams{Limit: 3}})
	if rec.requests[2].Header.Get("If-None-Match") != "" {
		t.Fatal("another URL has no ETag")
	}

	rec = newRecorder(t, "sets-page-1")
	client = testClient(rec)
	_, _ = client.Sets.List(ctx, nil)
	_, _ = client.Sets.List(ctx, nil)
	for _, req := range rec.requests {
		if req.Header.Get("If-None-Match") != "" {
			t.Fatal("no cache by default")
		}
	}
}

func TestReferenceSingleRequest(t *testing.T) {
	ctx := context.Background()
	rec := newRecorder(t, "reference")
	client := testClient(rec)

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := client.Reference.All(ctx); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	regions, _ := client.Reference.Regions(ctx)
	locales, _ := client.Reference.Locales(ctx)
	currencies, _ := client.Reference.Currencies(ctx)
	unknown, _ := client.Reference.Values(ctx, "nope")
	if !reflect.DeepEqual(regions, []string{"WEST", "EAST"}) ||
		!reflect.DeepEqual(locales, []string{"en", "it", "es", "fr", "de", "ja", "zh", "ko"}) ||
		!reflect.DeepEqual(currencies, []string{"EUR", "USD", "GBP", "JPY", "CHF"}) ||
		len(unknown) != 0 {
		t.Fatalf("vocabularies: %v %v %v %v", regions, locales, currencies, unknown)
	}
	all, _ := client.Reference.All(ctx)
	keys := make([]string, 0, len(all))
	for k := range all {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if !reflect.DeepEqual(keys, []string{"currencies", "locales", "regions", "scopes"}) {
		t.Fatalf("keys: %v", keys)
	}
	if rec.count() != 1 {
		t.Fatalf("requests: %d", rec.count())
	}

	// An error is not remembered: the next call retries.
	flaky := newRecorder(t, "502-html", "reference")
	client = testClient(flaky)
	if _, err := client.Reference.All(ctx); err == nil {
		t.Fatal("want the 502")
	}
	if regions, err := client.Reference.Regions(ctx); err != nil || len(regions) != 2 || flaky.count() != 2 {
		t.Fatalf("after error: %v %v %d", err, regions, flaky.count())
	}
}

func TestEnvelopeUnwrap(t *testing.T) {
	ctx := context.Background()
	api := func(name string) *Client { return testClient(newRecorder(t, name)) }

	games, err := api("games").Games.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, len(games))
	for i, g := range games {
		ids[i] = g.ID
	}
	if !reflect.DeepEqual(ids, GameIDs) {
		t.Fatalf("game ids: %v", ids)
	}
	for _, g := range games {
		if g.ID == GamePokemon && g.Name != "Pokémon TCG" {
			t.Fatalf("pokemon name: %q", g.Name)
		}
	}

	set, err := api("set-op01").Sets.Get(ctx, "OP01")
	if err != nil || set.Code != "OP01" || set.Region != RegionWest || !reflect.DeepEqual(set.Names, map[string]string{"en": "Romance Dawn", "ja": "ROMANCE DAWN"}) {
		t.Fatalf("set: %v %+v", err, set)
	}

	card, err := api("card-mams-en001").Cards.Get(ctx, "9e1c4a27-3b5d-4e8f-a6c1-2d7b9f0e3c58")
	if err != nil {
		t.Fatal(err)
	}
	if card.Number != "MAMS-EN001" || card.Name != "Blue-Eyes White Dragon, the White Phantom Beast" || card.Set.Code != "YGO-MAMS" {
		t.Fatalf("card: %+v", card)
	}
	if card.Names == nil || len(card.Names) != 0 {
		t.Fatalf("detail names must be an empty map, got %#v", card.Names)
	}
	if card.Attributes["attack"] != 3000.0 {
		t.Fatalf("attributes: %v", card.Attributes)
	}
	if card.ExternalIDs.Cardmarket != nil || card.ExternalIDs.TCGplayer == nil || *card.ExternalIDs.TCGplayer != 662107 {
		t.Fatalf("external ids: %+v", card.ExternalIDs)
	}

	found, err := api("cards-yugioh-mams-en001").Cards.List(ctx, &CardListParams{Game: GameYuGiOh, Number: "MAMS-EN001"})
	if err != nil || found.Data[0].Names != nil || found.HasMore() {
		t.Fatalf("list row: %v %+v", err, found)
	}

	zoro, err := api("card-op01-001").Cards.Get(ctx, "7a3d9e2b-5c1f-4b8a-9e6d-3f2a1c8b7e94")
	if err != nil || zoro.Name != "Roronoa Zoro" || zoro.Names["ja"] != "ロロノア・ゾロ" {
		t.Fatalf("zoro: %v %+v", err, zoro)
	}

	sealed, err := api("sealed-op01-booster-box").Sealed.Get(ctx, "c4e1a9f3-2d7b-4c5e-8a1f-9b3d6e2c7a15")
	if err != nil || sealed.Kind != "BOOSTER_BOX" || sealed.Set == nil || sealed.Set.Code == nil || *sealed.Set.Code != "OP01" {
		t.Fatalf("sealed: %v %+v", err, sealed)
	}
	sealedPage, err := api("sealed-page").Sealed.List(ctx, &SealedListParams{Game: GameOnePiece, PageParams: PageParams{Limit: 1}})
	if err != nil || sealedPage.Data[0].PackCount == nil || *sealedPage.Data[0].PackCount != 24 {
		t.Fatalf("sealed page: %v", err)
	}

	quotes, err := api("prices-card").Prices.Card(ctx, "7a3d9e2b-5c1f-4b8a-9e6d-3f2a1c8b7e94", nil)
	if err != nil {
		t.Fatal(err)
	}
	var triples []string
	for _, q := range quotes {
		triples = append(triples, q.Source+"/"+q.Variant+"/"+q.Price.Currency)
	}
	if !reflect.DeepEqual(triples, []string{"CARDMARKET/LOWEST_NEAR_MINT/EUR", "CARDMARKET/TREND/EUR", "TCGPLAYER/MARKET/USD"}) || quotes[0].Price.Amount != 0.35 {
		t.Fatalf("quotes: %v", triples)
	}
	if quotes[0].EUOnly || quotes[0].AsOf != "2026-11-18" {
		t.Fatalf("quote fields: %+v", quotes[0])
	}
	sealedQuotes, err := api("prices-sealed").Prices.Sealed(ctx, "c4e1a9f3-2d7b-4c5e-8a1f-9b3d6e2c7a15", nil)
	if err != nil || sealedQuotes[0].Condition != nil {
		t.Fatalf("sealed quotes: %v", err)
	}

	sources, err := api("prices-sources").Prices.Sources(ctx)
	if err != nil || len(sources) != 2 || sources[0].Code != "CARDMARKET" || sources[1].Code != "TCGPLAYER" {
		t.Fatalf("sources: %v %+v", err, sources)
	}
	if sources[0].DelayHours != 24 || sources[0].MaxAgeDays != 90 {
		t.Fatalf("source fields: %+v", sources[0])
	}
	coverage, err := api("prices-coverage").Prices.Coverage(ctx)
	if err != nil || coverage[0].Game != GameLorcana || coverage[0].AsOf != "2026-11-18" {
		t.Fatalf("coverage: %v %+v", err, coverage)
	}

	plans, err := api("plans").Plans(ctx)
	if err != nil {
		t.Fatal(err)
	}
	codes := make([]string, len(plans))
	for i, p := range plans {
		codes[i] = p.Code
	}
	if !reflect.DeepEqual(codes, []string{"FREE", "DEVELOPER", "GROWTH", "PROFESSIONAL", "ENTERPRISE"}) {
		t.Fatalf("plans: %v", codes)
	}
	if plans[0].PriceMonth == nil || plans[0].PriceMonth.Amount != 0 || plans[0].PriceMonth.Currency != "EUR" || plans[0].Renews || plans[4].Credits != nil {
		t.Fatalf("plan fields: %+v / %+v", plans[0], plans[4])
	}

	status, err := api("status").Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	lanes := make([]string, len(status.Refresh))
	for i, lane := range status.Refresh {
		lanes[i] = lane.Lane
	}
	if !reflect.DeepEqual(lanes, []string{"catalog", "prices"}) {
		t.Fatalf("status lanes: %v", lanes)
	}
	health, err := api("health").Health(ctx)
	if err != nil || *health != (Health{Status: "ok", DB: true, UptimeS: 86400, Version: "v0.1.0"}) {
		t.Fatalf("health: %v %+v", err, health)
	}
}

func TestPricesCurrentBatch(t *testing.T) {
	ctx := context.Background()
	const (
		zoroID    = "7a3d9e2b-5c1f-4b8a-9e6d-3f2a1c8b7e94"
		boxID     = "c4e1a9f3-2d7b-4c5e-8a1f-9b3d6e2c7a15"
		missingID = "00000000-0000-4000-8000-000000000000"
	)

	rec := newRecorder(t, "prices-current")
	result, err := testClient(rec).Prices.Current(ctx, []string{zoroID, boxID, missingID}, &PriceParams{Source: "CARDMARKET"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/v1/prices/current?ids=" + zoroID + "%2C" + boxID + "%2C" + missingID + "&source=CARDMARKET"}
	if got := rec.paths(); !reflect.DeepEqual(got, want) {
		t.Fatalf("paths:\n got %q\nwant %q", got, want)
	}
	if result.Meta != (CurrentPricesMeta{Requested: 3, Found: 2}) {
		t.Fatalf("meta: %+v", result.Meta)
	}
	items := make([]string, len(result.Data))
	for i, item := range result.Data {
		items[i] = fmt.Sprintf("%s/%s/%d", item.ID, item.Kind, len(item.Quotes))
	}
	if !reflect.DeepEqual(items, []string{zoroID + "/card/2", boxID + "/sealed/1"}) {
		t.Fatalf("items: %v", items)
	}
	if result.Data[0].Quotes[1].Price.Currency != "USD" {
		t.Fatalf("second quote: %+v", result.Data[0].Quotes[1])
	}
	wholeBody(t, "prices-current", result)

	// No ids is nothing to ask, too many is refused: neither reaches the wire.
	idle := newRecorder(t)
	empty, err := testClient(idle).Prices.Current(ctx, []string{}, nil)
	if err != nil || empty == nil || empty.Data == nil || len(empty.Data) != 0 || empty.Meta != (CurrentPricesMeta{}) {
		t.Fatalf("no ids: %v %+v", err, empty)
	}
	tooMany := make([]string, MaxCurrentIDs+1)
	for i := range tooMany {
		tooMany[i] = zoroID
	}
	refused, err := testClient(idle).Prices.Current(ctx, tooMany, nil)
	if err == nil || refused != nil {
		t.Fatalf("too many ids: %v %+v", err, refused)
	}
	if msg := err.Error(); !strings.Contains(msg, "at most 25 ids") || !strings.Contains(msg, "received 26") {
		t.Fatalf("too many ids, message: %q", msg)
	}
	if idle.count() != 0 {
		t.Fatalf("the guards made %d requests", idle.count())
	}
}

func TestPricesHistory(t *testing.T) {
	ctx := context.Background()
	const (
		zoroID = "7a3d9e2b-5c1f-4b8a-9e6d-3f2a1c8b7e94"
		boxID  = "c4e1a9f3-2d7b-4c5e-8a1f-9b3d6e2c7a15"
	)

	rec := newRecorder(t, "prices-history-card", "prices-history-sealed")
	client := testClient(rec)
	card, err := client.Prices.CardHistory(ctx, zoroID, nil)
	if err != nil {
		t.Fatal(err)
	}
	box, err := client.Prices.SealedHistory(ctx, boxID, &PriceHistoryParams{From: "2026-09-01", To: "2026-11-19", Bucket: "month", Variant: "LOWEST"})
	if err != nil {
		t.Fatal(err)
	}
	// url.Values writes the keys in alphabetical order: the same pairs the
	// other SDKs send, in another sequence.
	want := []string{
		"/v1/cards/" + zoroID + "/prices/history",
		"/v1/sealed/" + boxID + "/prices/history?bucket=month&from=2026-09-01&to=2026-11-19&variant=LOWEST",
	}
	if got := rec.paths(); !reflect.DeepEqual(got, want) {
		t.Fatalf("paths:\n got %q\nwant %q", got, want)
	}

	// The trial key asked for 90 days and reads 7: the answer is shorter and
	// says so, it is not an error.
	wantMeta := PriceHistoryMeta{
		ID: zoroID, Kind: "card", From: "2026-11-12", To: "2026-11-19", Bucket: "day", Count: 5,
		Truncated: true, Capped: false, PlanWindowDays: ptr(7),
	}
	if !reflect.DeepEqual(card.Meta, wantMeta) {
		t.Fatalf("card meta:\n got %+v\nwant %+v", card.Meta, wantMeta)
	}
	if len(card.Data) != 5 {
		t.Fatalf("card points: %d", len(card.Data))
	}
	if first := card.Data[0]; first.Date != "2026-11-16" || first.Source != "CARDMARKET" || first.Variant != "TREND" || first.Price.Amount != 0.5 || first.EUOnly {
		t.Fatalf("first point: %+v", first)
	}
	wholeBody(t, "prices-history-card", card)

	if box.Meta.PlanWindowDays != nil {
		t.Fatalf("a plan with no window has a nil PlanWindowDays, got %d", *box.Meta.PlanWindowDays)
	}
	if box.Meta.Bucket != "month" || box.Meta.Truncated || box.Meta.Capped {
		t.Fatalf("box meta: %+v", box.Meta)
	}
	dates := make([]string, len(box.Data))
	for i, point := range box.Data {
		dates[i] = point.Date
	}
	if !reflect.DeepEqual(dates, []string{"2026-09-01", "2026-10-01", "2026-11-01"}) {
		t.Fatalf("box dates: %v", dates)
	}
	wholeBody(t, "prices-history-sealed", box)
}

func TestRouteTable(t *testing.T) {
	ctx := context.Background()
	const id = "ID"
	rec := newRecorder(t, emptyList())
	c := testClient(rec)
	calls := []func() error{
		func() error { _, err := c.Health(ctx); return err },
		func() error { _, err := c.Status(ctx); return err },
		func() error { _, err := c.Reference.All(ctx); return err },
		func() error { _, err := c.Plans(ctx); return err },
		func() error { _, err := c.Prices.Sources(ctx); return err },
		func() error { _, err := c.Prices.Coverage(ctx); return err },
		func() error { _, err := c.Games.List(ctx); return err },
		func() error { _, err := c.Sets.List(ctx, nil); return err },
		func() error { _, err := c.Sets.Get(ctx, id); return err },
		func() error { _, err := c.Sets.Cards(ctx, id, nil); return err },
		func() error { _, err := c.Cards.List(ctx, nil); return err },
		func() error { _, err := c.Cards.Get(ctx, id); return err },
		func() error { _, err := c.Sealed.List(ctx, nil); return err },
		func() error { _, err := c.Sealed.Get(ctx, id); return err },
		func() error { _, err := c.Prices.Card(ctx, id, nil); return err },
		func() error { _, err := c.Prices.Sealed(ctx, id, nil); return err },
		func() error { _, err := c.Prices.Current(ctx, []string{id}, nil); return err },
		func() error { _, err := c.Prices.CardHistory(ctx, id, nil); return err },
		func() error { _, err := c.Prices.SealedHistory(ctx, id, nil); return err },
	}
	for _, call := range calls {
		// The decode of an empty list into a single object may fail: only the route matters here.
		_ = call()
	}
	hit := make([]string, 0, len(rec.requests))
	seen := map[string]bool{}
	for _, req := range rec.requests {
		segments := strings.Split(req.URL.Path, "/")
		for i, s := range segments {
			if s == id {
				segments[i] = "{id}"
			}
		}
		route := "GET " + strings.Join(segments, "/")
		if seen[route] {
			t.Fatalf("route hit twice: %s", route)
		}
		seen[route] = true
		hit = append(hit, route)
	}
	var want []string
	if err := json.Unmarshal(mustRead(t, filepath.Join(fixturesDir(t), "routes.json")), &want); err != nil {
		t.Fatal(err)
	}
	sort.Strings(hit)
	sort.Strings(want)
	if !reflect.DeepEqual(hit, want) {
		t.Fatalf("routes:\n got %v\nwant %v", hit, want)
	}
}

func TestTimeoutMapsToTimeoutError(t *testing.T) {
	attempts := 0
	hang := answer(func(req *http.Request) (*http.Response, error) {
		attempts++
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	noSleep(t)
	_, err := testClient(newRecorder(t, hang), WithMaxRetries(1), WithTimeout(10*time.Millisecond)).Games.List(context.Background())
	var timeout *TimeoutError
	if !errors.As(err, &timeout) {
		t.Fatalf("want TimeoutError, got %T %v", err, err)
	}
	var conn *ConnectionError
	if !errors.As(err, &conn) {
		t.Fatal("TimeoutError must unwrap to *ConnectionError")
	}
	if attempts != 2 {
		t.Fatalf("attempts: %d", attempts)
	}

	// A cancelled caller context is returned as is, not as a timeout, and not retried.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	attempts = 0
	_, err = testClient(newRecorder(t, hang), WithMaxRetries(3)).Games.List(ctx)
	if !errors.Is(err, context.Canceled) || errors.As(err, &timeout) || attempts > 1 {
		t.Fatalf("cancelled: %T %v after %d attempts", err, err, attempts)
	}
}

func TestFixtures(t *testing.T) {
	var routes []string
	if err := json.Unmarshal(mustRead(t, filepath.Join(fixturesDir(t), "routes.json")), &routes); err != nil || len(routes) != 19 {
		t.Fatalf("routes.json: %v, %d", err, len(routes))
	}
	if doc := loadDoc(t, "games"); doc.Status != 200 {
		t.Fatalf("games status %d", doc.Status)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
