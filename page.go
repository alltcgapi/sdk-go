package alltcgapi

import (
	"context"
	"iter"
)

// Page is one page of a collection that knows how to fetch the next one.
//
// The cursor in links.next belongs to the sort order of the list that issued
// it, so it must never be rebuilt by hand: Next requests exactly the URL the
// API returned, which is the failure mode this type exists to avoid.
type Page[T any] struct {
	Data []T
	Meta CollectionMeta
	// NextURL is links.next as it arrived, or empty on the last page.
	NextURL string

	client *Client
}

func newPage[T any](c *Client, body Collection[T]) *Page[T] {
	p := &Page[T]{Data: body.Data, Meta: body.Meta, client: c}
	if body.Links.Next != nil {
		p.NextURL = *body.Links.Next
	}
	return p
}

// HasMore reports whether the API returned a links.next URL.
func (p *Page[T]) HasMore() bool { return p.NextURL != "" }

// Next fetches the following page, or returns (nil, nil) when there is none.
func (p *Page[T]) Next(ctx context.Context) (*Page[T], error) {
	if p.NextURL == "" {
		return nil, nil
	}
	var body Collection[T]
	if err := p.client.follow(ctx, p.NextURL, &body); err != nil {
		return nil, err
	}
	return newPage(p.client, body), nil
}

// All walks every item of every page, starting from this one.
//
//	for card, err := range page.All(ctx) {
//	    if err != nil { return err }
//	    ...
//	}
func (p *Page[T]) All(ctx context.Context) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		page := p
		for page != nil {
			for _, item := range page.Data {
				if !yield(item, nil) {
					return
				}
			}
			next, err := page.Next(ctx)
			if err != nil {
				var zero T
				yield(zero, err)
				return
			}
			page = next
		}
	}
}

// Collect materialises up to maxItems items. The ceiling is mandatory because
// the catalogue has hundreds of thousands of cards and every page costs
// credits: an unbounded collect is the fastest way to spend them all by mistake.
func (p *Page[T]) Collect(ctx context.Context, maxItems int) ([]T, error) {
	out := make([]T, 0, min(max(0, maxItems), len(p.Data)))
	if maxItems <= 0 {
		return out, nil
	}
	for item, err := range p.All(ctx) {
		if err != nil {
			return out, err
		}
		out = append(out, item)
		if len(out) >= maxItems {
			break
		}
	}
	return out, nil
}
