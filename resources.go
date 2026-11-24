package alltcgapi

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// GamesService covers /v1/games.
type GamesService struct{ client *Client }

// List returns the games with their counts. Game.ID is the value of game=
// everywhere. 1 credit.
func (s *GamesService) List(ctx context.Context) ([]Game, error) {
	var out envelope[[]Game]
	if err := s.client.get(ctx, "/v1/games", nil, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// SetsService covers /v1/sets.
type SetsService struct{ client *Client }

// List returns sets, newest first. Region tells the Western line (WEST) from
// the Asian one (EAST). 1 credit.
func (s *SetsService) List(ctx context.Context, params *SetListParams) (*Page[CardSet], error) {
	var body Collection[CardSet]
	if err := s.client.get(ctx, "/v1/sets", params.values(), &body); err != nil {
		return nil, err
	}
	return newPage(s.client, body), nil
}

// Get returns one set by uuid or by code (OP01, YGO-MAMS), with its names per
// language. 1 credit.
func (s *SetsService) Get(ctx context.Context, idOrCode string) (*CardSet, error) {
	var out envelope[CardSet]
	if err := s.client.get(ctx, pathf("/v1/sets/%s", idOrCode), nil, &out); err != nil {
		return nil, err
	}
	return &out.Data, nil
}

// Cards returns the cards of a set in collector-number order. 1 credit per 50
// rows requested.
func (s *SetsService) Cards(ctx context.Context, idOrCode string, params *SetCardsParams) (*Page[Card], error) {
	var body Collection[Card]
	if err := s.client.get(ctx, pathf("/v1/sets/%s/cards", idOrCode), params.values(), &body); err != nil {
		return nil, err
	}
	return newPage(s.client, body), nil
}

// CardsService covers /v1/cards.
type CardsService struct{ client *Client }

// List returns cards across the catalogue. Number is exact, as printed
// (MAMS-EN001); Name is a prefix. 1 credit per 50 rows requested.
func (s *CardsService) List(ctx context.Context, params *CardListParams) (*Page[Card], error) {
	var body Collection[Card]
	if err := s.client.get(ctx, "/v1/cards", params.values(), &body); err != nil {
		return nil, err
	}
	return newPage(s.client, body), nil
}

// Get returns one card by id, with its names per language. 1 credit.
func (s *CardsService) Get(ctx context.Context, id string) (*Card, error) {
	var out envelope[Card]
	if err := s.client.get(ctx, pathf("/v1/cards/%s", id), nil, &out); err != nil {
		return nil, err
	}
	return &out.Data, nil
}

// SealedService covers /v1/sealed: booster boxes, starter decks, collections.
type SealedService struct{ client *Client }

// List returns sealed products. 1 credit per 50 rows requested.
func (s *SealedService) List(ctx context.Context, params *SealedListParams) (*Page[SealedProduct], error) {
	var body Collection[SealedProduct]
	if err := s.client.get(ctx, "/v1/sealed", params.values(), &body); err != nil {
		return nil, err
	}
	return newPage(s.client, body), nil
}

// Get returns one sealed product. 1 credit.
func (s *SealedService) Get(ctx context.Context, id string) (*SealedProduct, error) {
	var out envelope[SealedProduct]
	if err := s.client.get(ctx, pathf("/v1/sealed/%s", id), nil, &out); err != nil {
		return nil, err
	}
	return &out.Data, nil
}

// MaxCurrentIDs is how many ids Prices.Current accepts in one call: the API's
// ceiling.
const MaxCurrentIDs = 25

// PricesService returns current quotes and their history. Each row says where
// it comes from (Source), what it measures (Variant) and which day it is of
// (AsOf, Date); amount and currency always travel together and are never
// converted.
type PricesService struct{ client *Client }

// Card returns the current quotes of a card. 2 credits. A card with no quote
// is an empty slice, not a 404.
func (s *PricesService) Card(ctx context.Context, id string, params *PriceParams) ([]Quote, error) {
	var out envelope[[]Quote]
	if err := s.client.get(ctx, pathf("/v1/cards/%s/prices", id), params.values(), &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// Sealed returns the current quotes of a sealed product. 2 credits.
func (s *PricesService) Sealed(ctx context.Context, id string, params *PriceParams) ([]Quote, error) {
	var out envelope[[]Quote]
	if err := s.client.get(ctx, pathf("/v1/sealed/%s/prices", id), params.values(), &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// Current returns the current quotes of several items in one call, cards and
// sealed products together: 4 credits whatever the number of ids, up to
// MaxCurrentIDs. An id the API does not know is missing from Data without
// failing the others: Meta.Requested and Meta.Found say how many are missing.
// No ids is an empty result and no request; more than MaxCurrentIDs is an
// error returned before any request.
func (s *PricesService) Current(ctx context.Context, ids []string, params *PriceParams) (*CurrentPrices, error) {
	// With no ids there is nothing to ask: the API would answer 400.
	if len(ids) == 0 {
		return &CurrentPrices{Data: []CurrentPricesItem{}}, nil
	}
	if len(ids) > MaxCurrentIDs {
		return nil, fmt.Errorf("alltcgapi: Prices.Current accepts at most %d ids, received %d: chunk the list", MaxCurrentIDs, len(ids))
	}
	q := params.values()
	q.Set("ids", strings.Join(ids, ","))
	var out CurrentPrices
	if err := s.client.get(ctx, "/v1/prices/current", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CardHistory returns the price history of a card: one point per series per
// day, or per week or month with Bucket. 5 credits. With no dates it is the
// last 90 days. Past the days the plan reads the answer is not an error: it
// comes back shorter, and Meta.Truncated says so.
func (s *PricesService) CardHistory(ctx context.Context, id string, params *PriceHistoryParams) (*PriceHistory, error) {
	var out PriceHistory
	if err := s.client.get(ctx, pathf("/v1/cards/%s/prices/history", id), params.values(), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SealedHistory returns the price history of a sealed product, by the same
// rules as CardHistory. 5 credits.
func (s *PricesService) SealedHistory(ctx context.Context, id string, params *PriceHistoryParams) (*PriceHistory, error) {
	var out PriceHistory
	if err := s.client.get(ctx, pathf("/v1/sealed/%s/prices/history", id), params.values(), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Sources returns the price sources the API serves, each with its delay. Public.
func (s *PricesService) Sources(ctx context.Context) ([]PriceSource, error) {
	var out envelope[[]PriceSource]
	if err := s.client.get(ctx, "/v1/prices/sources", nil, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// Coverage says, per game and source, how many items have a quote. Public.
func (s *PricesService) Coverage(ctx context.Context) ([]PriceCoverage, error) {
	var out envelope[[]PriceCoverage]
	if err := s.client.get(ctx, "/v1/prices/coverage", nil, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// ReferenceService returns the closed vocabularies, to fill the filters of a
// UI without guessing strings. One request per client, whichever method is
// called and however often: the answer is the same for everyone.
type ReferenceService struct {
	client *Client
	mu     sync.Mutex
	cache  Reference
}

// All returns every vocabulary at once. Public. An error is not remembered:
// the next call retries. Concurrent first calls share one request.
func (s *ReferenceService) All(ctx context.Context) (Reference, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cache != nil {
		return s.cache, nil
	}
	var out envelope[Reference]
	if err := s.client.get(ctx, "/v1/reference", nil, &out); err != nil {
		return nil, err
	}
	if out.Data == nil {
		out.Data = Reference{}
	}
	s.cache = out.Data
	return s.cache, nil
}

// Values returns one vocabulary by name; an unknown one is an empty slice.
func (s *ReferenceService) Values(ctx context.Context, key string) ([]string, error) {
	all, err := s.All(ctx)
	if err != nil {
		return nil, err
	}
	return append([]string{}, all[key]...), nil
}

// Regions returns the print lines (WEST, EAST).
func (s *ReferenceService) Regions(ctx context.Context) ([]string, error) {
	return s.Values(ctx, "regions")
}

// Locales returns the languages names and quotes can come in.
func (s *ReferenceService) Locales(ctx context.Context) ([]string, error) {
	return s.Values(ctx, "locales")
}

// Currencies returns the currencies quotes can come in.
func (s *ReferenceService) Currencies(ctx context.Context) ([]string, error) {
	return s.Values(ctx, "currencies")
}
