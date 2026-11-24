# alltcgapi for Go

Go client for [alltcgapi](https://alltcgapi.com/?utm_source=go&utm_medium=readme&utm_campaign=sdk_go), the trading card game API: nineteen games, one key, one response shape. Cards, sets, sealed products, current prices with their source and their history, for Pokémon, Yu-Gi-Oh!, One Piece, Lorcana, Digimon, Flesh and Blood, Star Wars: Unlimited and more.

- Standard library only. Go 1.23 or later (range-over-func iterators).
- Typed responses; typed errors that work with `errors.As`, each carrying `Code` and `RequestID`.
- Pagination that follows `links.next` for you.
- Retries with `Retry-After` on rate limits and 5xx, never on quota or an expired trial.

```sh
go get github.com/alltcgapi/sdk-go
```

## Quick start

Get a key from the [free API key page](https://alltcgapi.com/free-api-key?utm_source=go&utm_medium=readme&utm_campaign=sdk_go) and put it in `ALLTCG_API_KEY`.

```go
package main

import (
	"context"
	"fmt"
	"log"

	alltcgapi "github.com/alltcgapi/sdk-go"
)

func main() {
	ctx := context.Background()
	client := alltcgapi.New() // reads ALLTCG_API_KEY

	// Find a Yu-Gi-Oh! card by the number printed on it.
	page, err := client.Cards.List(ctx, &alltcgapi.CardListParams{Game: alltcgapi.GameYuGiOh, Number: "MAMS-EN001"})
	if err != nil {
		log.Fatal(err)
	}
	card := page.Data[0]
	fmt.Println(card.Name) // Blue-Eyes White Dragon, the White Phantom Beast

	// Its current quotes: one row per source, variant and language, never blended or converted.
	quotes, err := client.Prices.Card(ctx, card.ID, nil)
	if err != nil {
		log.Fatal(err)
	}
	for _, q := range quotes {
		fmt.Println(q.Source, q.Variant, q.Price.Amount, q.Price.Currency, q.AsOf)
	}
}
```

## Prices over time

`AsOf` and `Date` are days in UTC (`YYYY-MM-DD`), not instants. `Prices.Sources` says how many hours each source is delayed.

```go
// One point per series per day; week and month buckets carry the median of their days.
history, err := client.Prices.CardHistory(ctx, card.ID, &alltcgapi.PriceHistoryParams{From: "2026-08-01", Bucket: "week", Source: "TCGPLAYER"})
if err != nil {
	log.Fatal(err)
}
for _, point := range history.Data {
	fmt.Println(point.Date, point.Variant, point.Price.Amount, point.Price.Currency)
}
if history.Meta.Truncated {
	// The plan reads back PlanWindowDays days: the answer is shorter, not an error.
	fmt.Println("history starts at", history.Meta.From)
}

// Up to 25 cards and sealed products in one request, for a flat 4 credits.
current, err := client.Prices.Current(ctx, []string{card.ID, boxID}, nil)
if err != nil {
	log.Fatal(err)
}
fmt.Println(current.Meta.Found, "of", current.Meta.Requested)
```

Without `From` and `To` the history covers the last 90 days. `Meta.Capped` is `true` when a series was too long for one response: ask for `Bucket: "week"` or a shorter window.

## Walk a set

List methods return a `*Page[T]`. `All` walks every page, requesting each `links.next` URL exactly as the API returned it, so you never build a cursor by hand.

```go
page, err := client.Sets.Cards(ctx, "OP01", &alltcgapi.SetCardsParams{PageParams: alltcgapi.PageParams{Limit: 250}})
if err != nil {
	log.Fatal(err)
}
for card, err := range page.All(ctx) {
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(card.Number, card.Name) // OP01-001 Roronoa Zoro, ...
}
```

`page.Collect(ctx, max)` materialises a list and requires a ceiling: every page costs credits. `page.Next(ctx)` fetches one page at a time.

## Errors

Every API error unwraps to `*alltcgapi.APIError` with `Status`, `Code`, `Message`, `Details` and `RequestID`. Quote the `RequestID` in a support request. Three of them look alike and need opposite handling:

```go
var trial *alltcgapi.TrialExpiredError
var quota *alltcgapi.QuotaExceededError
var limited *alltcgapi.RateLimitedError
switch {
case errors.As(err, &trial):
	// 403 TRIAL_EXPIRED: the trial credits do not come back. See client.Plans.
case errors.As(err, &quota):
	// 429 QUOTA_EXCEEDED: the daily cap or the period is used up. Not retried.
case errors.As(err, &limited):
	// 429 RATE_LIMITED or CONCURRENCY_LIMIT after the retries ran out; limited.RetryAfter
}
```

| Type | When |
| --- | --- |
| `*AuthenticationError` | 401: missing, malformed, revoked or expired key |
| `*PermissionDeniedError` | 403: the key may not do this |
| `*TrialExpiredError` | 403 `TRIAL_EXPIRED` (unwraps to `*PermissionDeniedError`) |
| `*NotFoundError` | 404, or any `*_NOT_FOUND` code |
| `*InvalidRequestError` | 400: unknown or repeated parameter, value outside its vocabulary. `Param()` names it |
| `*RateLimitedError` | 429 `RATE_LIMITED`, `CONCURRENCY_LIMIT` |
| `*QuotaExceededError` | 429 `QUOTA_EXCEEDED` |
| `*ServerError` | 5xx, including 503 `METER_UNAVAILABLE` |
| `*ConnectionError`, `*TimeoutError` | no answer, or no answer within the timeout |

A cancelled `context.Context` comes back as `ctx.Err()`, untouched.

## Credits and quota

Every response carries what the call cost and what is left. Read the last one, or count them all:

```go
client := alltcgapi.New(alltcgapi.WithOnResponse(func(info alltcgapi.ResponseInfo) {
	if info.QuotaRemaining != nil {
		log.Println("credits left:", *info.QuotaRemaining)
	}
}))
```

`client.LastResponse()` returns the headers of the last response. With `WithETagCache()` the client sends `If-None-Match` and serves a `304` from memory: a 304 costs no credits.

## Options

| Option | Default | |
| --- | --- | --- |
| `WithAPIKey` | `ALLTCG_API_KEY` from the environment | sent as `X-Api-Key` |
| `WithBaseURL` | `https://api.alltcgapi.com` | |
| `WithTimeout` | 30 s | per attempt |
| `WithMaxRetries` | 2 | repeated attempts after the first; 0 disables |
| `WithETagCache` | off | conditional requests |
| `WithUserAgent` | `alltcgapi-go/<version>` | |
| `WithHTTPClient` | `&http.Client{}` | to inject a transport |
| `WithOnResponse` | | called for every response, errors included |

## Methods

| Method | Route |
| --- | --- |
| `Games.List(ctx)` | `GET /v1/games` |
| `Sets.List(ctx, *SetListParams)` | `GET /v1/sets` |
| `Sets.Get(ctx, idOrCode)` | `GET /v1/sets/{id}` |
| `Sets.Cards(ctx, idOrCode, *SetCardsParams)` | `GET /v1/sets/{id}/cards` |
| `Cards.List(ctx, *CardListParams)` | `GET /v1/cards` |
| `Cards.Get(ctx, id)` | `GET /v1/cards/{id}` |
| `Sealed.List(ctx, *SealedListParams)` | `GET /v1/sealed` |
| `Sealed.Get(ctx, id)` | `GET /v1/sealed/{id}` |
| `Prices.Card(ctx, id, *PriceParams)` | `GET /v1/cards/{id}/prices` |
| `Prices.Sealed(ctx, id, *PriceParams)` | `GET /v1/sealed/{id}/prices` |
| `Prices.Current(ctx, ids, *PriceParams)` | `GET /v1/prices/current` |
| `Prices.CardHistory(ctx, id, *PriceHistoryParams)` | `GET /v1/cards/{id}/prices/history` |
| `Prices.SealedHistory(ctx, id, *PriceHistoryParams)` | `GET /v1/sealed/{id}/prices/history` |
| `Prices.Sources(ctx)` | `GET /v1/prices/sources` |
| `Prices.Coverage(ctx)` | `GET /v1/prices/coverage` |
| `Reference.All(ctx)`, `.Regions`, `.Locales`, `.Currencies` | `GET /v1/reference` (one request per client) |
| `Plans(ctx)` | `GET /v1/billing/plans` |
| `Status(ctx)` | `GET /v1/status` |
| `Health(ctx)` | `GET /v1/health` |

Params may be `nil`. `PriceParams` filters by `Source`, `Language` and `Variant`; `PriceHistoryParams` adds `From`, `To` and `Bucket`. The game ids are constants (`GamePokemon`, `GameYuGiOh`, `GameOnePiece`, ...) and all nineteen are in `GameIDs`.

## Links

- [Documentation](https://alltcgapi.com/docs?utm_source=go&utm_medium=readme&utm_campaign=sdk_go)
- [API reference](https://alltcgapi.com/docs/api?utm_source=go&utm_medium=readme&utm_campaign=sdk_go)
- [Pricing](https://alltcgapi.com/pricing?utm_source=go&utm_medium=readme&utm_campaign=sdk_go)
- [Status](https://alltcgapi.com/status?utm_source=go&utm_medium=readme&utm_campaign=sdk_go)

Game names, card names and card images belong to their owners. alltcgapi is independent and is not endorsed by any game publisher or marketplace.

## License

MIT
