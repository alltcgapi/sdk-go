// Package alltcgapi is a thin client for the trading card game API at
// https://alltcgapi.com: nineteen games, one key, one response shape. It
// covers games, sets, cards, sealed products, current prices with their
// source and their history, price coverage, the price list, the reference
// vocabularies, status and health.
//
// It has no dependencies outside the standard library. Every method takes a
// context.Context; list methods return a *Page that follows the API's own
// links.next URL, so pagination never has to be rebuilt by hand.
package alltcgapi

// Version is the SDK version. It travels in the User-Agent header of every
// request and must match the git tag of a release.
const Version = "0.1.0"

const defaultUserAgent = "alltcgapi-go/" + Version
