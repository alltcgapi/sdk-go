package alltcgapi

import (
	"fmt"
	"net/url"
	"strconv"
)

// query builds the query string the API expects: zero values are left out
// entirely. No parameter of this API is a repeated key (that is a 400): the
// one list, the ids of Prices.Current, travels as a single ids key joined by
// commas. Keys go out in alphabetical order, as url.Values encodes them.
type query struct{ url.Values }

func newQuery() query { return query{url.Values{}} }

func (q query) str(key, value string) {
	if value != "" {
		q.Set(key, value)
	}
}

func (q query) int(key string, value int) {
	if value != 0 {
		q.Set(key, strconv.Itoa(value))
	}
}

// pathf fills a path template such as "/v1/cards/%s/prices" with URL-escaped
// segments. The template is kept literal so that the route table can be
// audited by reading the source.
func pathf(template string, segments ...string) string {
	escaped := make([]any, len(segments))
	for i, s := range segments {
		escaped[i] = url.PathEscape(s)
	}
	return fmt.Sprintf(template, escaped...)
}

// PageParams are the parameters every list shares.
type PageParams struct {
	// Limit is 1 to 250, default 50. Above 250 is a 400 LIMIT_EXCEEDED, never a
	// truncation. A list costs 1 credit per 50 rows REQUESTED.
	Limit int
	// Cursor is the cursor of a previous links.next, unchanged. Page follows it on its own.
	Cursor string
}

func (p PageParams) add(q query) {
	q.int("limit", p.Limit)
	q.str("cursor", p.Cursor)
}

// SetListParams filters /v1/sets.
type SetListParams struct {
	Game string
	// Region is RegionWest or RegionEast.
	Region string
	PageParams
}

func (p *SetListParams) values() url.Values {
	q := newQuery()
	if p != nil {
		q.str("game", p.Game)
		q.str("region", p.Region)
		p.add(q)
	}
	return q.Values
}

// CardListParams filters /v1/cards.
type CardListParams struct {
	Game string
	// Set is a set id or code.
	Set string
	// Name matches the start of the card name.
	Name string
	// Number is the collector number, exactly as printed.
	Number string
	PageParams
}

func (p *CardListParams) values() url.Values {
	q := newQuery()
	if p != nil {
		q.str("game", p.Game)
		q.str("set", p.Set)
		q.str("name", p.Name)
		q.str("number", p.Number)
		p.add(q)
	}
	return q.Values
}

// SetCardsParams filters /v1/sets/{id}/cards.
type SetCardsParams struct {
	Name   string
	Number string
	PageParams
}

func (p *SetCardsParams) values() url.Values {
	q := newQuery()
	if p != nil {
		q.str("name", p.Name)
		q.str("number", p.Number)
		p.add(q)
	}
	return q.Values
}

// SealedListParams filters /v1/sealed.
type SealedListParams struct {
	Game string
	Set  string
	// Kind is a product kind, such as BOOSTER_BOX.
	Kind string
	PageParams
}

func (p *SealedListParams) values() url.Values {
	q := newQuery()
	if p != nil {
		q.str("game", p.Game)
		q.str("set", p.Set)
		q.str("kind", p.Kind)
		p.add(q)
	}
	return q.Values
}

// PriceParams filters current quotes.
type PriceParams struct {
	// Source keeps one source. An unknown source is a 400 that lists the available ones.
	Source string
	// Language keeps one language, such as en or ja.
	Language string
	// Variant keeps one variant, such as MARKET, TREND or LOWEST_NEAR_MINT.
	Variant string
}

func (p *PriceParams) values() url.Values {
	q := newQuery()
	if p != nil {
		q.str("source", p.Source)
		q.str("language", p.Language)
		q.str("variant", p.Variant)
	}
	return q.Values
}

// PriceHistoryParams sets the window and the bucket of a price history and
// filters its series. The dates are YYYY-MM-DD strings in UTC and go out as
// given: the SDK never computes them.
type PriceHistoryParams struct {
	// From is the first day. With neither From nor To: the last 90 days.
	From string
	// To is the last day.
	To string
	// Bucket is day (the default), week or month: weeks and months carry the median of their days.
	Bucket string
	// Source keeps one source. An unknown source is a 400 that lists the available ones.
	Source string
	// Language keeps one language, such as en or ja.
	Language string
	// Variant keeps one variant, such as MARKET, TREND or LOWEST_NEAR_MINT.
	Variant string
}

func (p *PriceHistoryParams) values() url.Values {
	q := newQuery()
	if p != nil {
		q.str("from", p.From)
		q.str("to", p.To)
		q.str("bucket", p.Bucket)
		q.str("source", p.Source)
		q.str("language", p.Language)
		q.str("variant", p.Variant)
	}
	return q.Values
}
