package alltcgapi

// Response types, written by hand field by field from
// sdk-typescript/src/types.ts, in the same order. A nullable field is a
// pointer: the service always writes the key, and null means "not known",
// which is information. Names is nil on list rows and a non-nil (possibly
// empty) map on detail responses.

// The ids of /v1/games: the value of game= everywhere.
const (
	GameBattleSpirits     = "battlespirits"
	GameCyberpunk         = "cyberpunk"
	GameDigimon           = "digimon"
	GameDragoborne        = "dragoborne"
	GameDragonBallFW      = "dragonball-fw"
	GameDragonBallMasters = "dragonball-masters"
	GameFleshAndBlood     = "fab"
	GameFinalFantasy      = "fftcg"
	GameForceOfWill       = "fow"
	GameGundam            = "gundam"
	GameLorcana           = "lorcana"
	GameOnePiece          = "onepiece"
	GamePokemon           = "pokemon"
	GameRiftbound         = "riftbound"
	GameStarWarsDestiny   = "swdestiny"
	GameStarWarsUnlimited = "swu"
	GameVanguard          = "vanguard"
	GameWeissSchwarz      = "weissschwarz"
	GameYuGiOh            = "yugioh"
)

// GameIDs are the nineteen games, in the order of /v1/games.
var GameIDs = []string{
	GameBattleSpirits, GameCyberpunk, GameDigimon, GameDragoborne, GameDragonBallFW,
	GameDragonBallMasters, GameFleshAndBlood, GameFinalFantasy, GameForceOfWill, GameGundam,
	GameLorcana, GameOnePiece, GamePokemon, GameRiftbound, GameStarWarsDestiny,
	GameStarWarsUnlimited, GameVanguard, GameWeissSchwarz, GameYuGiOh,
}

// The print line of a set.
const (
	RegionWest = "WEST"
	RegionEast = "EAST"
)

// Money is an amount and its currency, always together and never converted.
type Money struct {
	Amount float64 `json:"amount"`
	// Currency is ISO 4217.
	Currency string `json:"currency"`
}

// ExternalIDs are the marketplace ids: the bridge to the inventory you already have.
type ExternalIDs struct {
	Cardmarket *int64 `json:"cardmarket"`
	TCGplayer  *int64 `json:"tcgplayer"`
}

// Game is one row of /v1/games. ID is the value to pass as game=.
type Game struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Sets   int    `json:"sets"`
	Cards  int    `json:"cards"`
	Sealed int    `json:"sealed"`
}

// CardSet is a set.
type CardSet struct {
	ID   string  `json:"id"`
	Game string  `json:"game"`
	Code string  `json:"code"`
	Slug *string `json:"slug"`
	Name string  `json:"name"`
	// Names is the name per language. Detail responses only; empty when there are none.
	Names  map[string]string `json:"names,omitempty"`
	Region string            `json:"region"`
	// ReleaseDate is YYYY-MM-DD.
	ReleaseDate  *string  `json:"release_date"`
	TotalCards   *int     `json:"total_cards"`
	PrintedCards *int     `json:"printed_cards"`
	ImageURL     *string  `json:"image_url"`
	Languages    []string `json:"languages"`
	// Version moves only when the data of the object changes.
	Version   int    `json:"version"`
	UpdatedAt string `json:"updated_at"`
}

// CardSetRef is the set inside a card.
type CardSetRef struct {
	ID   string `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

// Card is one printing.
type Card struct {
	// ID is stable: it never changes and is never reused.
	ID   string     `json:"id"`
	Game string     `json:"game"`
	Set  CardSetRef `json:"set"`
	// Number is the number as printed: OP01-001, MAMS-EN001.
	Number string `json:"number"`
	Name   string `json:"name"`
	// Names is the name per language. Detail responses only; empty when there are none.
	Names     map[string]string `json:"names,omitempty"`
	Rarity    *string           `json:"rarity"`
	Supertype *string           `json:"supertype"`
	// Attributes is the game's rules data: one object for every game, different keys per game.
	Attributes  map[string]any `json:"attributes"`
	ImageURL    *string        `json:"image_url"`
	Languages   []string       `json:"languages"`
	ExternalIDs ExternalIDs    `json:"external_ids"`
	Version     int            `json:"version"`
	UpdatedAt   string         `json:"updated_at"`
}

// SealedSetRef is the set inside a sealed product: its code can be missing.
type SealedSetRef struct {
	ID   string  `json:"id"`
	Code *string `json:"code"`
}

// SealedProduct is a booster box, a starter deck, a collection.
type SealedProduct struct {
	ID   string        `json:"id"`
	Game *string       `json:"game"`
	Set  *SealedSetRef `json:"set"`
	SKU  string        `json:"sku"`
	// Kind is BOOSTER_BOX and the like.
	Kind        string      `json:"kind"`
	Name        string      `json:"name"`
	ImageURL    *string     `json:"image_url"`
	ReleaseDate *string     `json:"release_date"`
	PackCount   *int        `json:"pack_count"`
	Languages   []string    `json:"languages"`
	ExternalIDs ExternalIDs `json:"external_ids"`
	Version     int         `json:"version"`
	UpdatedAt   string      `json:"updated_at"`
}

// PriceSeries is one price series: one source, one variant, one language, one
// condition, one printing, one region, and the amount in its currency. It is
// the part a current quote (Quote) and a point of the history (PricePoint)
// have in common. Both embed it, so its fields read as theirs: quote.Source,
// point.Price.
type PriceSeries struct {
	// Source is CARDMARKET or TCGPLAYER: Prices.Sources says which are served.
	Source string `json:"source"`
	// Variant is what the number measures: LOWEST_NEAR_MINT, LOWEST, MARKET, TREND, AVG_7D, AVG_30D.
	Variant   string  `json:"variant"`
	Language  string  `json:"language"`
	Condition *string `json:"condition"`
	Printing  *string `json:"printing"`
	Region    *string `json:"region"`
	// EUOnly is true when the source counted only the sellers of the European Union.
	EUOnly bool  `json:"eu_only"`
	Price  Money `json:"price"`
}

// Quote is one current quote: a series and the day the source last quoted it.
// A card usually has several: the API returns them all and does not blend
// them.
type Quote struct {
	PriceSeries
	// AsOf is a day, YYYY-MM-DD in UTC: not an instant.
	AsOf string `json:"as_of"`
}

// PricePoint is one point of the history: the price of a series on a day (or
// in the week or the month that starts on that day).
type PricePoint struct {
	// Date is YYYY-MM-DD in UTC.
	Date string `json:"date"`
	PriceSeries
}

// PriceHistoryMeta says which window a history covers and what the plan did
// to it.
type PriceHistoryMeta struct {
	ID string `json:"id"`
	// Kind is card or sealed.
	Kind string `json:"kind"`
	// From and To are the window served, which can be shorter than the one asked for: see Truncated.
	From string `json:"from"`
	To   string `json:"to"`
	// Bucket is day, week or month.
	Bucket string `json:"bucket"`
	Count  int    `json:"count"`
	// Truncated is true when the plan shortened the window asked for. The remedy is a plan with more days.
	Truncated bool `json:"truncated"`
	// Capped is true when the series was too long for one response. The remedy is Bucket week or a shorter window.
	Capped bool `json:"capped"`
	// PlanWindowDays is how many days of history the plan reads; nil means the whole history.
	PlanWindowDays *int `json:"plan_window_days"`
}

// PriceHistory is what Prices.CardHistory and Prices.SealedHistory return: the
// points and what the plan did to them.
type PriceHistory struct {
	Data []PricePoint     `json:"data"`
	Meta PriceHistoryMeta `json:"meta"`
}

// CurrentPricesItem is one item of Prices.Current, with its current quotes.
type CurrentPricesItem struct {
	ID string `json:"id"`
	// Kind is card or sealed.
	Kind   string  `json:"kind"`
	Quotes []Quote `json:"quotes"`
}

// CurrentPricesMeta counts the ids asked for and the ones the API knows.
type CurrentPricesMeta struct {
	Requested int `json:"requested"`
	Found     int `json:"found"`
}

// CurrentPrices is what Prices.Current returns: an id the API does not know is
// missing from Data, and Meta says how many are missing.
type CurrentPrices struct {
	Data []CurrentPricesItem `json:"data"`
	Meta CurrentPricesMeta   `json:"meta"`
}

// PriceSource is a price source the API serves.
type PriceSource struct {
	Code  string `json:"code"`
	Label string `json:"label"`
	// DelayHours is how many hours what the API serves lags behind the source.
	DelayHours int `json:"delay_hours"`
	// MaxAgeDays is after how many days without observations a price stops being served as current. The history keeps it.
	MaxAgeDays int `json:"max_age_days"`
}

// PriceCoverage says, per game and source, how many items have a quote and the day of the newest.
type PriceCoverage struct {
	Game   string `json:"game"`
	Source string `json:"source"`
	Cards  int    `json:"cards"`
	Sealed int    `json:"sealed"`
	// AsOf is a day, YYYY-MM-DD in UTC.
	AsOf string `json:"as_of"`
}

// Plan is one row of the price list.
type Plan struct {
	Code  string `json:"code"`
	Label string `json:"label"`
	// PriceMonth and PriceYear are nil where the price is under contract or the period does not exist.
	PriceMonth *Money `json:"price_month"`
	PriceYear  *Money `json:"price_year"`
	// Credits per period; nil means unlimited.
	Credits *int64 `json:"credits"`
	// Renews is false on the trial: its credits do not come back.
	Renews       bool   `json:"renews"`
	DailyCap     *int64 `json:"daily_cap"`
	RateLimitRPS int    `json:"rate_limit_rps"`
	RateLimitRPM int    `json:"rate_limit_rpm"`
	MaxKeys      int    `json:"max_keys"`
	HistoryDays  *int   `json:"history_days"`
}

// RefreshLane says when one refresh lane last finished.
type RefreshLane struct {
	Lane       string `json:"lane"`
	FinishedAt string `json:"finished_at"`
	Rows       int64  `json:"rows"`
}

// CatalogStatus is /v1/status: how fresh what the API serves is.
type CatalogStatus struct {
	Version string        `json:"version"`
	Refresh []RefreshLane `json:"refresh"`
}

// Health is /v1/health. It has no data envelope: it is a monitor's answer.
type Health struct {
	Status  string `json:"status"`
	DB      bool   `json:"db"`
	UptimeS int64  `json:"uptime_s"`
	Version string `json:"version"`
}

// Reference is /v1/reference: the closed vocabularies (regions, locales, currencies, scopes).
type Reference map[string][]string

// CollectionMeta is the meta block of a list.
type CollectionMeta struct {
	Limit int `json:"limit"`
	Count int `json:"count"`
	// TotalCount is set only where the service really counts.
	TotalCount *int `json:"total_count,omitempty"`
	HasMore    bool `json:"has_more"`
}

// Collection is the body of a list response.
type Collection[T any] struct {
	Data  []T            `json:"data"`
	Meta  CollectionMeta `json:"meta"`
	Links struct {
		Next *string `json:"next"`
	} `json:"links"`
}

type envelope[T any] struct {
	Data T `json:"data"`
}
