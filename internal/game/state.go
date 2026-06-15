// Package game owns all authoritative game state and logic. A Room runs in a
// single goroutine that serializes commands, joins, leaves and ticks, which
// keeps the game logic free of data races (see room.go).
package game

import "time"

// Tunable starting conditions / balance — all in one place.
const (
	StartingCash    = 3_000.0 // some money, no resources, no factories
	TickInterval    = 500 * time.Millisecond
	NewsCost        = 0.0  // posting to your own agency is free
	HackShock       = 0.25 // confidence drop inflicted on the victim
	RepairCostPerHP = 6.0
	MaxPlayers      = 12

	// Service prices, quoted in the universal "general unit". Each player pays in
	// their own currency: countryCost = generalCost / exchangeRate (a stronger
	// currency buys these for less). See convertCost.
	CostAgency   = 50.0
	CostMilitary = 800.0
	SpyCost      = 300.0
	HackCost     = 25.0
)

// Conn is the server's view of a connection. The hub's websocket client
// implements it; game code only ever needs to push bytes out.
type Conn interface {
	Send([]byte)
	Close()
}

// Config holds the host-chosen room settings (zero fields fall back to the
// package defaults via withDefaults).
type Config struct {
	StartingCash  float64 // initial treasury per player
	NukeWin       int     // nuclear weapons needed to win (the only win condition)
	SecondWorldAt float64 // capital to reach Second World
	FirstWorldAt  float64 // capital to reach First World
}

func (c Config) withDefaults() Config {
	if c.StartingCash <= 0 {
		c.StartingCash = StartingCash
	}
	if c.NukeWin <= 0 {
		c.NukeWin = NukeWinCount
	}
	if c.SecondWorldAt <= 0 {
		c.SecondWorldAt = SecondWorldDefault
	}
	if c.FirstWorldAt <= c.SecondWorldAt {
		c.FirstWorldAt = c.SecondWorldAt * 2
	}
	return c
}

// Room holds the full state of one game instance, keyed by name in the Hub.
type Room struct {
	Name     string
	Cfg      Config
	Players  map[string]*Player // keyed by player ID
	order    []string           // stable player ordering for display
	Market   *Market
	News     []*NewsItem
	Tick     int
	Started  bool
	HostID   string // the player who controls the lobby start
	WinnerID string

	byConn     map[Conn]*Player
	over       bool
	activeTick int // last tick anyone was connected (seeded at creation)

	// OnEmpty is invoked (from the room goroutine) when the last player leaves,
	// letting the hub drop the room from its registry.
	OnEmpty func(name string)

	// channels driven by the single owning goroutine
	commands   chan Command
	register   chan *JoinReq
	unregister chan Conn
	done       chan struct{}
}

// Player is one country. It may have several live connections at once — e.g. a
// map tab and a console tab — all belonging to the same browser session, keyed
// by Token. A different session reusing the country name is rejected.
type Player struct {
	ID           string
	Name         string
	Token        string // session token shared by this player's views
	conns        map[Conn]bool
	Country      *Country
	Cash         float64
	Resources    map[string]int // integer stockpile of each resource
	Confidence   float64        // C_i, ~0.6..1.4, decays toward 1.0
	PrevCapital  float64        // for momentum M_i
	SpyAccess    map[string]int // targetID -> tick the access expires
	hasSatellite bool           // First-World satellite: sees every rival's map
	dcTick       int            // tick when the last connection dropped (while offline)
}

// nukeCount is the number of surviving nuclear weapons. Each nuke is a placeable
// on the map: a damaged one still counts, a destroyed one (HP<=0) does not.
func (p *Player) nukeCount() int {
	n := 0
	for _, pl := range p.Country.Placeables {
		if pl.Kind == KindNuke && !pl.destroyed() {
			n++
		}
	}
	return n
}

func (p *Player) addConn(c Conn) {
	if p.conns == nil {
		p.conns = map[Conn]bool{}
	}
	p.conns[c] = true
}

func (p *Player) dropConn(c Conn) { delete(p.conns, c) }

func (p *Player) connected() bool { return len(p.conns) > 0 }

type Country struct {
	Name         string
	Currency     string
	Seed         int64
	Palette      string
	Boundary     [][2]float64 // normalized boundary points
	Placeables   map[string]*Placeable
	exchangeRate float64
}

type PlaceableKind string

const (
	KindFactory    PlaceableKind = "factory"
	KindNewsAgency PlaceableKind = "agency"
	KindMilitary   PlaceableKind = "military"
	KindNuke       PlaceableKind = "nuke"
)

type Placeable struct {
	ID      string        `json:"id"`
	Kind    PlaceableKind `json:"kind"`
	Subtype string        `json:"subtype"`
	Icon    string        `json:"icon"`
	X       float64       `json:"x"`
	Y       float64       `json:"y"`
	HP      float64       `json:"hp"` // 0..100
	MaxHP   float64       `json:"maxHp"`
	Value   float64       `json:"-"` // true value, never sent to attackers
	Payout  float64       `json:"-"` // factory cash per production cycle

	// Cooldown: ticks until the building is "ready" again (produce / post / fire).
	Cooldown    int `json:"cooldown"`
	CooldownMax int `json:"cooldownMax"`
}

// healthFrac is 0..1; damaged buildings are less effective.
func (p *Placeable) healthFrac() float64 {
	if p.MaxHP <= 0 {
		return 0
	}
	return p.HP / p.MaxHP
}

func (p *Placeable) destroyed() bool { return p.HP <= 0 }

type Commodity struct {
	Name     string    `json:"name"`
	Demand   float64   `json:"-"`
	BaseCost float64   `json:"-"`
	Price    float64   `json:"price"`
	History  []float64 `json:"-"`
}

type Market struct {
	Commodities map[string]*Commodity
}

type NewsItem struct {
	ID         string `json:"id"`
	SourceID   string `json:"source"`
	SourceName string `json:"sourceName"`
	Headline   string `json:"headline"`
	Body       string `json:"body"`
	Fake       bool   `json:"-"` // server-only; players never see this flag
	Tick       int    `json:"tick"`
}

// Command is one decoded client message routed into the owning room.
type Command struct {
	Conn Conn
	Type string
	Raw  []byte
}

// JoinReq is a registration request. A player's identity is just their country
// name; the currency is derived from it and (after the game starts) the name is
// the key used to resume a dropped player. Reply receives nil on success or a
// rejection error, so the caller can surface it to the client synchronously.
type JoinReq struct {
	Conn        Conn
	CountryName string
	Token       string
	Reply       chan error
}

func (j *JoinReq) reply(err error) {
	if j.Reply != nil {
		j.Reply <- err
	}
}

// rawAssets is the world-priced value of a player's resource stock and factories
// (health-scaled). Strategic buildings (news agency, military) and nukes are not
// counted.
func (p *Player) rawAssets(m *Market) float64 {
	raw := 0.0
	for name, n := range p.Resources {
		raw += float64(n) * priceOf(m, name)
	}
	for _, pl := range p.Country.Placeables {
		if pl.Kind == KindFactory && !pl.destroyed() {
			raw += pl.Value * pl.healthFrac()
		}
	}
	return raw
}

// capital is net wealth in the player's currency: cash + assets converted by
// exchange rate (the same calculation as resource and service prices), so
// trading is value-neutral. Used for display and the win check.
func (p *Player) capital(m *Market) float64 {
	return p.Cash + p.convertCost(p.rawAssets(m))
}

// grossCapital is wealth in world units (cash + asset value, NO currency
// conversion). The exchange rate is derived from this rather than from capital,
// so the rate never depends on itself — otherwise capital and the rate form a
// feedback loop that oscillates between two values each tick.
func (p *Player) grossCapital(m *Market) float64 {
	return p.Cash + p.rawAssets(m)
}
