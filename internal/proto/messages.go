// Package proto defines the WebSocket message envelope and payload types
// exchanged between the browser client and the authoritative Go server.
package proto

import "encoding/json"

// Envelope wraps every message. Type selects the payload shape.
type Envelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// ---- Client -> Server commands ----
const (
	CmdPlaceableMove  = "placeable.move"  // drag an icon to a new spot
	CmdMarketOrder    = "market.order"    // buy/sell a resource
	CmdBuyService     = "service.buy"     // build military / news agency (cash)
	CmdBuildFactory   = "factory.build"   // build a factory (consumes resources)
	CmdBuildNuke      = "nuke.build"      // assemble a nuclear weapon
	CmdRepair         = "repair"          // pay to repair a damaged placeable
	CmdAttack         = "attack"          // strike a target placeable
	CmdSpy            = "spy"             // pay to reveal a rival's map
	CmdPublishNews    = "news.publish"    // post to your own agency (free)
	CmdHackNews       = "news.hack"       // plant fake news on a rival
	CmdStartGame      = "game.start"      // host starts the match from the lobby
	CmdCloseRoom      = "room.close"      // host tears the whole room down
	CmdBuildSatellite = "satellite.build" // First-World satellite: reveal all maps
)

type PlaceableMove struct {
	ID string  `json:"id"`
	X  float64 `json:"x"` // normalized 0..1 within the country bounds
	Y  float64 `json:"y"`
}

type MarketOrder struct {
	Commodity string  `json:"commodity"` // resource name
	Side      string  `json:"side"`      // "buy" | "sell"
	Qty       float64 `json:"qty"`
}

// BuyService builds a new cash-funded placeable (military or agency).
type BuyService struct {
	Kind    string `json:"kind"`    // "military" | "agency"
	Subtype string `json:"subtype"` // e.g. "army", "press"
}

// BuildFactory constructs a factory by consuming its resource recipe.
type BuildFactory struct {
	Type string `json:"type"` // factory key, e.g. "jewelry"
}

type Repair struct {
	ID string `json:"id"`
}

type Attack struct {
	TargetPlayer    string  `json:"targetPlayer"`
	TargetPlaceable string  `json:"targetPlaceable"`
	Spend           float64 `json:"spend"` // military budget committed
}

type Spy struct {
	TargetPlayer string `json:"targetPlayer"`
}

type PublishNews struct {
	Headline string `json:"headline"`
	Body     string `json:"body"`
}

type HackNews struct {
	TargetPlayer string `json:"targetPlayer"`
	Headline     string `json:"headline"`
	Body         string `json:"body"`
}

// ---- Server -> Client events ----
const (
	EvtSnapshot   = "snapshot"      // full state on join
	EvtTick       = "tick"          // per-tick diff: prices, currencies, capital
	EvtMapUpdate  = "map.update"    // your placeables changed
	EvtNews       = "news.item"     // a new headline hit the feed
	EvtCombat     = "combat.result" // damage report
	EvtSpyReveal  = "spy.reveal"    // a rival's true map + values (you paid)
	EvtTell       = "tell"          // ambient "someone is close" signal
	EvtWin        = "win"           // game over, winner + revealed objectives
	EvtRoomClosed = "room.closed"   // the host closed the room; clients should not reconnect
	EvtNotice     = "notice"        // personal toast (money earned/lost, errors)
	EvtError      = "error"
)
