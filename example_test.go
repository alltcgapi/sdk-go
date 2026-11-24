package alltcgapi_test

import (
	"context"
	"errors"
	"fmt"
	"log"

	alltcgapi "github.com/alltcgapi/sdk-go"
)

// Find a Yu-Gi-Oh! card by its printed number, then read its current quotes.
func Example() {
	ctx := context.Background()
	client := alltcgapi.New() // reads ALLTCG_API_KEY from the environment

	page, err := client.Cards.List(ctx, &alltcgapi.CardListParams{
		Game:   alltcgapi.GameYuGiOh,
		Number: "MAMS-EN001",
	})
	if err != nil {
		log.Fatal(err)
	}
	if len(page.Data) == 0 {
		return
	}
	card := page.Data[0]
	quotes, err := client.Prices.Card(ctx, card.ID, nil)
	if err != nil {
		log.Fatal(err)
	}
	for _, q := range quotes {
		fmt.Println(q.Source, q.Variant, q.Price.Amount, q.Price.Currency)
	}
}

// Read the weekly prices of a card from one source, then the current quotes of
// a card and a sealed product in one request.
func ExamplePricesService_CardHistory() {
	ctx := context.Background()
	client := alltcgapi.New()
	const (
		cardID = "7a3d9e2b-5c1f-4b8a-9e6d-3f2a1c8b7e94"
		boxID  = "c4e1a9f3-2d7b-4c5e-8a1f-9b3d6e2c7a15"
	)

	history, err := client.Prices.CardHistory(ctx, cardID, &alltcgapi.PriceHistoryParams{
		From:   "2026-08-01",
		Bucket: "week",
		Source: "TCGPLAYER",
	})
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

	current, err := client.Prices.Current(ctx, []string{cardID, boxID}, nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(current.Meta.Found, "of", current.Meta.Requested)
}

// Walk every card of a One Piece set; the page follows links.next on its own.
func ExamplePage_All() {
	ctx := context.Background()
	client := alltcgapi.New()

	page, err := client.Sets.Cards(ctx, "OP01", nil)
	if err != nil {
		log.Fatal(err)
	}
	for card, err := range page.All(ctx) {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(card.Number, card.Name)
	}
}

// Tell the three 429/403 cases apart: wait, stop for today, or upgrade.
func ExampleTrialExpiredError() {
	ctx := context.Background()
	client := alltcgapi.New()

	_, err := client.Cards.Get(ctx, "7a3d9e2b-5c1f-4b8a-9e6d-3f2a1c8b7e94")
	var trial *alltcgapi.TrialExpiredError
	var quota *alltcgapi.QuotaExceededError
	var api *alltcgapi.APIError
	switch {
	case errors.As(err, &trial):
		fmt.Println("trial credits used up: pick a plan")
	case errors.As(err, &quota):
		fmt.Println("quota reached: try again after the reset")
	case errors.As(err, &api):
		fmt.Println(api.Code, api.RequestID)
	}
}
