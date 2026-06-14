# Geo-Economic Strategy Game — Design & Implementation Plan

### Go · Server-Side Rendering · WebSockets

This document is a build-ready plan for a 5+ player multiplayer geo-economic strategy game. It assumes the game-design decisions already made:

- **5+ players**, each controls a country with cash, resources, a currency, and a news agency.
- **Hidden objectives** — every player has a secret win condition.
- **Silent race** — the first player to _complete and declare_ their objective wins instantly; counterplay happens _before_ completion, driven by detectable "tells."
- **Live economy** — commodity price `P = (D / S) × C`.
- **Currency** — exchange rate `E = (K / K̄) × C × M`, where `K` = capital (cash + resource value), `K̄` = average capital, `C` = manipulable confidence, `M` = momentum.

---

## 1. Technology stack

| Layer               | Choice                                                             | Why                                                                                                                     |
| ------------------- | ------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------- |
| Language            | **Go 1.22+**                                                       | Goroutines map naturally onto per-room game loops and per-connection sockets.                                           |
| HTTP router         | **net/http** (stdlib, 1.22 pattern routing)                        | No dependency needed; `mux.HandleFunc("GET /room/{name}", …)` is enough.                                                |
| Realtime            | **github.com/coder/websocket**                                     | Modern, context-aware, handles concurrent writes safely, actively maintained. (Gorilla is archived.)                    |
| SSR templates       | **github.com/a-h/templ**                                           | Type-safe Go components, compile-time checked, composable. Falls back cleanly to `html/template` if you want zero deps. |
| Lobby interactivity | **HTMX** (optional)                                                | Server-rendered create/join forms without hand-written fetch code. The _game_ uses raw WS, not HTMX.                    |
| Live charts         | **uPlot** (tiny, fast) and/or **lightweight-charts** (TradingView) | Sub-millisecond redraws for live price/currency figures.                                                                |
| Map + icons         | **Inline SVG** + **Pointer Events**                                | Procedural country shapes, draggable resource/agency/military icons, CSS filters for "fancy."                           |
| Persistence         | **In-memory** (v1), optional **SQLite** later                      | Rooms are ephemeral; no accounts needed.                                                                                |

**SSR clarification.** "SSR" here means the _entry points_ — landing page, create-room, join-room, and the game shell — are rendered as full HTML on the server. Once the game page loads, it opens a single WebSocket and the live game state (prices, maps, news, combat) streams over that socket and is painted client-side. The server is **authoritative**: it owns all state and validates every command.

---

## 2. High-level architecture

```
┌──────────────────────────────────────────────────────────────┐
│                          Browser                                │
│  SSR pages (templ)        Game shell + client.js                │
│  ┌───────────┐            ┌───────────────────────────────┐    │
│  │ Landing   │            │  SVG map  │ uPlot charts       │    │
│  │ Create/Join│  HTTP ──▶ │  Drag-drop icons               │    │
│  └───────────┘            │  News feed (badge)             │    │
│                           └──────────────┬────────────────┘    │
└──────────────────────────────────────────┼─────────────────────┘
                                            │ WebSocket (JSON)
┌───────────────────────────────────────────▼────────────────────┐
│                          Go server                               │
│  net/http  ─▶  Hub (rooms by name)                               │
│                  │                                               │
│                  ├─ Room "atlas"  ── goroutine ── game loop      │
│                  │     owns: players, market, news, objectives   │
│                  │     ticker (500ms) → recompute → broadcast    │
│                  └─ Room "vega"   ── goroutine ── game loop      │
└──────────────────────────────────────────────────────────────────┘
```

**Concurrency model (the key decision).** Each room runs in **one goroutine** that owns all of that room's state. Nothing else touches that state directly. Connections send _commands_ into the room over a channel; the room broadcasts _snapshots/events_ back out through each connection's send channel. This is the standard Go hub pattern and it removes almost all locking from game logic — the only shared structure is the hub's `map[string]*Room`, guarded by a mutex.

---

## 3. Project structure

```
geo-game/
├── go.mod
├── cmd/
│   └── server/
│       └── main.go            # wiring: router, hub, static, templ pages
├── internal/
│   ├── hub/
│   │   ├── hub.go             # room registry (create/get by name)
│   │   ├── room.go            # per-room goroutine + game loop
│   │   └── client.go          # per-connection read/write pumps
│   ├── game/
│   │   ├── state.go           # Room state, Player, Country structs
│   │   ├── economy.go         # price + currency formulas, tick
│   │   ├── market.go          # buy/sell, supply/demand bookkeeping
│   │   ├── combat.go          # attack resolution
│   │   ├── espionage.go       # spy view, fog-of-war
│   │   ├── news.go            # news items, fake news, confidence shocks
│   │   ├── objectives.go      # hidden goals + win detection
│   │   └── mapgen.go          # procedural country shapes
│   └── proto/
│       └── messages.go        # WS message envelope + payload types
├── web/
│   ├── pages/                 # templ components (.templ → generated .go)
│   │   ├── landing.templ
│   │   ├── room.templ
│   │   └── game.templ
│   └── static/
│       ├── css/app.css        # the "fancy" theme
│       └── js/
│           ├── client.js      # WS client + dispatch
│           ├── map.js         # SVG map render + drag-drop
│           ├── charts.js      # uPlot live figures
│           └── news.js        # feed panel + badge
└── README.md
```

`go.mod` dependencies:

```
require (
    github.com/coder/websocket v1.8.x
    github.com/a-h/templ        v0.3.x
)
```

---

## 4. The WebSocket message protocol

Every message is a JSON envelope. The `type` selects the payload shape.

```go
// internal/proto/messages.go
package proto

import "encoding/json"

type Envelope struct {
    Type    string          `json:"type"`
    Payload json.RawMessage `json:"payload"`
}

// ---- Client → Server ----
const (
    CmdPlaceableMove = "placeable.move"  // drag an icon to a new spot
    CmdMarketOrder   = "market.order"    // buy/sell a commodity
    CmdBuyService    = "service.buy"      // military / spy / agency upgrade
    CmdAttack        = "attack"           // strike a target placeable
    CmdSpy           = "spy"              // pay to reveal a rival's map
    CmdPublishNews   = "news.publish"     // post to your own agency (free)
    CmdHackNews      = "news.hack"        // plant fake news on a rival
    CmdDeclareWin    = "win.declare"      // attempt instant win
)

type PlaceableMove struct {
    ID string  `json:"id"`
    X  float64 `json:"x"` // normalized 0..1 within the country bounds
    Y  float64 `json:"y"`
}

type MarketOrder struct {
    Commodity string  `json:"commodity"`
    Side      string  `json:"side"` // "buy" | "sell"
    Qty       float64 `json:"qty"`
}

type Attack struct {
    TargetPlayer    string  `json:"targetPlayer"`
    TargetPlaceable string  `json:"targetPlaceable"`
    Spend           float64 `json:"spend"` // military budget committed
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

// ---- Server → Client ----
const (
    EvtSnapshot   = "snapshot"     // full state on join
    EvtTick       = "tick"          // per-tick diff: prices, currencies, capital
    EvtMapUpdate  = "map.update"    // your placeables changed (or a spied map)
    EvtNews       = "news.item"     // a new headline hit the feed
    EvtCombat     = "combat.result" // damage report
    EvtSpyReveal  = "spy.reveal"    // a rival's true map + values (you paid)
    EvtTell       = "tell"          // ambient "someone is close" signal
    EvtWin        = "win"           // game over, winner + revealed objectives
    EvtError      = "error"
)
```

A few protocol rules:

- The server **never** trusts client-sent money, prices, or damage. The client sends _intent_ (`buy 10 oil`); the server computes the result against authoritative state and broadcasts it.
- Position updates use **normalized coordinates** (0..1) so they're resolution-independent across screens.
- Fog-of-war is enforced server-side: a `map.update` for a rival's country is only ever sent in response to a paid `spy` command (`EvtSpyReveal`), and **prices/values are stripped** from the attack-targeting view.

---

## 5. Core data models

```go
// internal/game/state.go
package game

type Room struct {
    Name       string
    Players    map[string]*Player // keyed by player ID
    Market     *Market
    News       []*NewsItem
    Tick       int
    Started    bool
    WinnerID   string
}

type Player struct {
    ID         string
    Name       string
    Conn       *Client          // back-reference to the socket
    Country    *Country
    Cash       float64
    Confidence float64          // C_i, 0.6..1.4, decays toward 1.0
    PrevCapital float64         // for momentum M_i
    Objective  *Objective       // hidden
    ObjHeldTurns int            // turns the win condition has held
    SpyAccess  map[string]int   // targetID -> tick the access expires
}

type Country struct {
    Seed       int64            // drives the procedural shape
    Palette    string
    Placeables map[string]*Placeable
}

type PlaceableKind string

const (
    KindResource    PlaceableKind = "resource"
    KindNewsAgency  PlaceableKind = "agency"
    KindMilitary    PlaceableKind = "military"
)

type Placeable struct {
    ID       string        `json:"id"`
    Kind     PlaceableKind `json:"kind"`
    Subtype  string        `json:"subtype"` // "oil", "gold", "tier2-army", ...
    Icon     string        `json:"icon"`    // icon name for the client
    X, Y     float64       `json:"x"`
    HP       float64       `json:"hp"`      // 0..100, attack reduces it
    Value    float64       `json:"-"`       // true value (never sent to attackers)
    Output   float64       `json:"-"`       // per-tick resource production
}

type Commodity struct {
    Name      string
    Demand    float64 // D — sum of standing buy interest
    Supply    float64 // S — sum of production across all players
    BaseCost  float64 // C — base cost of production per unit
    Price     float64 // P — recomputed each tick
    History   []float64
}

type Market struct {
    Commodities map[string]*Commodity
}

type NewsItem struct {
    ID          string  `json:"id"`
    SourceID    string  `json:"source"`    // which agency/country
    SourceName  string  `json:"sourceName"`
    Headline    string  `json:"headline"`
    Body        string  `json:"body"`
    Fake        bool    `json:"-"`         // server-only; players can't see this flag
    Tick        int     `json:"tick"`
}
```

---

## 6. The economy engine

### 6.1 Commodity price — `P = (D / S) × C`

```go
// internal/game/economy.go
func (m *Market) recomputePrices() {
    for _, c := range m.Commodities {
        s := c.Supply
        if s <= 0 {
            s = 0.001 // avoid div-by-zero; scarcity spikes price
        }
        c.Price = (c.Demand / s) * c.BaseCost
        c.History = append(c.History, c.Price)
    }
}
```

`Supply` is the sum of every player's per-tick production of that commodity (their resource placeables' `Output`). `Demand` aggregates standing buy interest. When someone corners a commodity (buys most of the supply), `S` available to the market drops and `P` spikes — which is exactly the _tell_ that makes a "Monopolist" secret objective detectable.

### 6.2 Currency — `E = (K / K̄) × C × M`

```go
func (r *Room) recomputeCurrencies() {
    var total float64
    caps := make(map[string]float64, len(r.Players))
    for id, p := range r.Players {
        k := p.capital(r.Market) // cash + Σ(resource qty × world price)
        caps[id] = k
        total += k
    }
    avg := total / float64(len(r.Players))

    for id, p := range r.Players {
        k := caps[id]
        base := k / avg                            // relative strength
        // momentum: hot if rapidly accumulating
        var m float64 = 1.0
        if p.PrevCapital > 0 {
            m = 1 + 0.3*((k-p.PrevCapital)/p.PrevCapital)
        }
        rate := base * p.Confidence * clamp(m, 0.7, 1.4)
        p.Country.exchangeRate = clamp(rate, 0.4, 3.0)
        p.PrevCapital = k
        // confidence decays back toward 1.0 each tick (markets correct)
        p.Confidence += (1.0 - p.Confidence) * 0.05
    }
}
```

`Confidence` is the lever your information-warfare layer pulls: planting fake news on a rival (`news.hack`) applies a negative shock to their `Confidence`, devaluing their currency and raising their import costs — a bloodless economic attack. Won battles and resource discoveries nudge it back up.

### 6.3 The authoritative tick loop

```go
func (r *Room) tickLoop() {
    ticker := time.NewTicker(500 * time.Millisecond)
    defer ticker.Stop()
    for {
        select {
        case cmd := <-r.commands:
            r.handleCommand(cmd) // mutate state in this goroutine only
        case c := <-r.register:
            r.addPlayer(c)
        case c := <-r.unregister:
            r.removePlayer(c)
        case <-ticker.C:
            r.Tick++
            r.applyProduction()      // resources add to cash / supply
            r.Market.recomputePrices()
            r.recomputeCurrencies()
            r.checkObjectives()      // win detection + tells
            r.broadcastTick()        // diff to every player (fog-of-war applied)
        }
    }
}
```

Putting all mutation in this one goroutine is what keeps the game free of data races: commands, joins, leaves, and ticks are serialized by the `select`.

---

## 7. Combat, espionage, news

### 7.1 Combat resolution

Attacker commits a budget; defender's spend on that placeable resists it. Damage scales with the ratio, with diminishing returns so a small attack chips and an overwhelming one destroys.

```go
// internal/game/combat.go
func resolveAttack(spend, defense, hp float64) (newHP float64, destroyed bool) {
    ratio := spend / (defense + 1)
    damage := hp * (ratio / (ratio + 1)) // 0..hp, asymptotic
    newHP = hp - damage
    if newHP <= 1 {
        return 0, true
    }
    return newHP, false
}
```

Crucial fog-of-war rule: in the **attack-targeting view** the client receives placeables with `Value` and prices stripped — the attacker sees _that_ a target exists and its rough HP, not what it's worth. Those numbers are only revealed through a paid spy action. That asymmetry is the whole reason espionage is worth buying.

### 7.2 Espionage / fog-of-war

A player only ever receives their **own** full country in `map.update`. A paid `spy` command grants time-limited access (`SpyAccess[targetID] = currentTick + duration`); while it's active, the server sends that rival's true map and values via `EvtSpyReveal`. When it expires, the reveal stops.

### 7.3 News and fake news (free to post)

Posting to your own agency is free and just appends a `NewsItem` to the room feed and broadcasts `EvtNews`. Hacking a rival's agency (`news.hack`) posts an item whose `SourceID` is the _victim's_ agency, marks it `Fake` server-side (players never see that flag), and applies a confidence shock to whomever the story targets. The feed itself never distinguishes real from fake — figuring that out is the players' job.

---

## 8. Hidden objectives & win detection

Every objective is **machine-verifiable** and uses **relative thresholds** plus a **hold duration** (the hold is the reaction window that keeps an instant win fair).

```go
// internal/game/objectives.go
type Objective struct {
    Kind        string  // "hegemon", "monopolist", "conqueror", ...
    Param       string  // e.g. target commodity or target player
    HoldTurns   int     // must satisfy for this many consecutive ticks
}

func (r *Room) checkObjectives() {
    for _, p := range r.Players {
        if r.satisfies(p, p.Objective) {
            p.ObjHeldTurns++
            if p.ObjHeldTurns == p.Objective.HoldTurns-1 {
                r.emitTell(p) // fire ambient "someone is close" with HoldTurns-1
            }
            if p.ObjHeldTurns >= p.Objective.HoldTurns {
                r.win(p)      // instant win once the hold completes
            }
        } else {
            p.ObjHeldTurns = 0 // knocked back resets the clock
        }
    }
}

func (r *Room) satisfies(p *Player, o *Objective) bool {
    switch o.Kind {
    case "hegemon": // capital >= 2x the second-richest
        return p.capital(r.Market) >= 2*r.secondRichestCapital(p.ID)
    case "monopolist": // >= 60% of one commodity's global supply
        return r.supplyShare(p.ID, o.Param) >= 0.60
    case "hardCurrency": // strongest exchange rate of all players
        return r.hasHighestRate(p.ID)
    case "conqueror": // drive target below 25% of starting capital
        return r.Players[o.Param].capital(r.Market) <= 0.25*StartingCash
    // ... arms dealer, puppet master, spymaster, cartel boss
    }
    return false
}
```

The `EvtTell` event is what every other system already feeds: a hot currency, a price spike, a hacked agency, a troop buildup. The objective stays hidden; the _symptoms_ are public. That's the silent race working.

---

## 9. Procedural country maps (random + fancy)

Each player's country is generated from a `Seed`. The shape is an irregular blob: sample N angles around a center, jitter the radius, then let the client smooth the points into a flowing closed curve and fill it with a randomized gradient.

```go
// internal/game/mapgen.go
func GenerateCountry(seed int64) *Country {
    rng := rand.New(rand.NewSource(seed))
    return &Country{
        Seed:       seed,
        Palette:    palettes[rng.Intn(len(palettes))],
        Placeables: defaultPlaceables(rng), // 1 agency + starter resources
    }
}

// Returns normalized boundary points the client renders as a smooth path.
func BoundaryPoints(seed int64, n int) [][2]float64 {
    rng := rand.New(rand.NewSource(seed))
    pts := make([][2]float64, n)
    for i := 0; i < n; i++ {
        ang := float64(i) / float64(n) * 2 * math.Pi
        rad := 0.32 + rng.Float64()*0.14 // jittered radius
        pts[i] = [2]float64{0.5 + rad*math.Cos(ang), 0.5 + rad*math.Sin(ang)}
    }
    return pts
}
```

Client-side smoothing (Catmull-Rom → cubic Bézier) turns the jittered points into an organic landmass, then CSS/SVG filters add the "fancy":

```js
// web/static/js/map.js  (excerpt)
function smoothPath(pts) {
  // Catmull-Rom spline, closed
  const p = pts.map(([x, y]) => [x * W, y * H]);
  let d = `M ${p[0][0]} ${p[0][1]} `;
  for (let i = 0; i < p.length; i++) {
    const p0 = p[(i - 1 + p.length) % p.length],
      p1 = p[i];
    const p2 = p[(i + 1) % p.length],
      p3 = p[(i + 2) % p.length];
    const c1 = [p1[0] + (p2[0] - p0[0]) / 6, p1[1] + (p2[1] - p0[1]) / 6];
    const c2 = [p2[0] - (p3[0] - p1[0]) / 6, p2[1] - (p3[1] - p1[1]) / 6];
    d += `C ${c1[0]} ${c1[1]} ${c2[0]} ${c2[1]} ${p2[0]} ${p2[1]} `;
  }
  return d + "Z";
}
```

The landmass gets a radial gradient fill, a soft glow (`<feGaussianBlur>` drop-shadow), a subtle animated noise overlay, and a neon border. Because every country uses a different seed and palette, no two games look alike.

### Draggable icons

Resource/agency/military placeables are SVG `<g>` nodes positioned at normalized coords. Pointer Events handle the drag; on release the client sends a `placeable.move` so the server (and any active spies) stay in sync.

```js
function makeDraggable(el, id, svg) {
  el.addEventListener("pointerdown", (e) => {
    el.setPointerCapture(e.pointerId);
    const move = (ev) => {
      const r = svg.getBoundingClientRect();
      const x = (ev.clientX - r.left) / r.width;
      const y = (ev.clientY - r.top) / r.height;
      el.setAttribute("transform", `translate(${x * W} ${y * H})`);
      el._pos = { x, y };
    };
    const up = () => {
      el.removeEventListener("pointermove", move);
      ws.send(
        JSON.stringify({
          type: "placeable.move",
          payload: { id, x: el._pos.x, y: el._pos.y },
        }),
      );
    };
    el.addEventListener("pointermove", move);
    el.addEventListener("pointerup", up, { once: true });
  });
}
```

---

## 10. The news feed (icon + badge)

A fixed newspaper icon in the page corner carries an **unread-count badge**. Each `EvtNews` increments it; opening the panel lists every headline (real and fake, indistinguishable) with its source agency and tick, and resets the badge.

```js
// web/static/js/news.js
let unread = 0;
const badge = document.getElementById("news-badge");
const panel = document.getElementById("news-panel");

export function onNews(item) {
  prependItem(item); // render into the panel list
  if (!panel.classList.contains("open")) {
    unread++;
    badge.textContent = unread;
    badge.hidden = false;
  }
}
document.getElementById("news-btn").addEventListener("click", () => {
  panel.classList.toggle("open");
  if (panel.classList.contains("open")) {
    unread = 0;
    badge.hidden = true;
  }
});

// Composing is free — opens a dialog, sends straight to your own agency.
function publish(headline, body) {
  ws.send(
    JSON.stringify({ type: "news.publish", payload: { headline, body } }),
  );
}
```

```html
<button id="news-btn" class="news-fab" aria-label="News feed">
  <svg><!-- newspaper icon --></svg>
  <span id="news-badge" class="badge" hidden>0</span>
</button>
<aside id="news-panel" class="glass"></aside>
```

---

## 11. Lobby: create / join rooms by room name (unlisted, no password)

the player can decide a name for his country and a name for its currency when join or create a room

```go
// internal/hub/hub.go
type Hub struct {
    mu    sync.Mutex
    rooms map[string]*Room
}

func (h *Hub) CreateRoom(name string) (*Room, error) {
    h.mu.Lock()
    defer h.mu.Unlock()
    if name == "" || len(name) > 40 {
        return nil, ErrBadName
    }
    if _, exists := h.rooms[name]; exists {
        return nil, ErrNameTaken   // "name not available"
    }
    r := NewRoom(name)
    h.rooms[name] = r
    go r.tickLoop()                // each room is its own goroutine
    return r, nil
}

func (h *Hub) GetRoom(name string) (*Room, bool) {
    h.mu.Lock()
    defer h.mu.Unlock()
    r, ok := h.rooms[name]
    return r, ok
}
```

There is **no endpoint that lists rooms** — discovery is impossible without knowing the exact name, which gives you "unlisted, no password" for free. Create returns an error if the name is taken; join 404s if the name doesn't exist. (Optionally generate a short random room name so collisions are rare.)

The WS handler upgrades, registers the connection with the room, and starts the read/write pumps:

```go
// internal/hub/client.go (coder/websocket)
func (h *Hub) ServeWS(w http.ResponseWriter, req *http.Request) {
    name := req.PathValue("name")
    room, ok := h.GetRoom(name)
    if !ok { http.NotFound(w, req); return }

    c, err := websocket.Accept(w, req, &websocket.AcceptOptions{
        OriginPatterns: []string{"yourhost.example"},
    })
    if err != nil { return }

    cl := &Client{conn: c, room: room, send: make(chan []byte, 64)}
    room.register <- cl
    ctx := req.Context()
    go cl.writePump(ctx)
    cl.readPump(ctx) // blocks; on return, connection is done
    room.unregister <- cl
}

func (cl *Client) readPump(ctx context.Context) {
    for {
        var env proto.Envelope
        if err := wsjson.Read(ctx, cl.conn, &env); err != nil { return }
        cl.room.commands <- Command{Client: cl, Env: env}
    }
}

func (cl *Client) writePump(ctx context.Context) {
    for msg := range cl.send {
        if err := cl.conn.Write(ctx, websocket.MessageText, msg); err != nil { return }
    }
}
```

---

## 12. Starting conditions

Defined as constants so balance is one place to tune:
another way to accieve money is to add resources and time to time it increase some small money for player (the player will be notified in his notification which say this value in his currancy is earned from that resource)

```go
const (
    StartingCash   = 10_000.0  // every player starts equal
    TickInterval   = 500 * time.Millisecond
    SpyDuration    = 20        // ticks of revealed access per purchase
    NewsCost       = 0.0       // posting news is free
)

func (r *Room) addPlayer(c *Client) {
    seed := time.Now().UnixNano() + int64(len(r.Players))
    country := game.GenerateCountry(seed)
    country.Placeables["agency-1"] = &game.Placeable{ // default news agency
        ID: "agency-1", Kind: game.KindNewsAgency, Icon: "newspaper",
        X: 0.5, Y: 0.5, HP: 100,
    }
    p := &game.Player{
        ID: newID(), Conn: c, Country: country,
        Cash: StartingCash, Confidence: 1.0,
        Objective: r.assignHiddenObjective(), // secret
    }
    r.Players[p.ID] = p
    c.send <- snapshotFor(p) // EvtSnapshot, fog-of-war applied
}
```

Each player begins with equal cash, one news agency, a random starter set of resource nodes, and a secret objective. Posting news costs nothing.

---

## 13. The "fancy" visual direction

A single coherent theme rather than scattered effects:

- **Dark glassmorphism** base — translucent panels (`backdrop-filter: blur`), thin luminous borders, deep navy/charcoal background.
- **Neon accent per country** — each player's palette drives their map glow, currency line color, and agency badge, so the board reads at a glance.
- **Live financial figures** — uPlot price and exchange-rate charts that animate on every tick; a "currency board" strip (like an airport departures board) ranking all players' rates, which doubles as the silent-race tell display.
- **Motion with restraint** — smooth transitions on placement and combat, a pulse on the news badge when something arrives, a brief screen-edge flare when an `EvtTell` fires ("someone is close").
- **Map polish** — gradient-filled landmass, soft outer glow, animated subtle noise/water texture around the coastline, icons that lift slightly on hover and snap on drop.

Keep it consistent: two font weights, one accent ramp per player, flat surfaces plus glass — not a pile of unrelated effects.

---

## 14. Implementation roadmap

**Phase 1 — Skeleton (rooms + sockets).** net/http + templ landing/create/join pages, Hub with create/join-by-name, coder/websocket upgrade, room goroutine with a no-op tick that broadcasts the player count. _Goal: two browsers in the same named room see each other._

**Phase 2 — Map + drag-drop.** Procedural country generation, SVG render with smoothing, draggable placeables, `placeable.move` round-trip. _Goal: place and drag your resources/agency/military icons._

**Phase 3 — Economy.** Commodities, the price tick, the buy/sell market order, capital + currency computation, uPlot live charts and the currency board. _Goal: prices move as players trade._

**Phase 4 — Conflict & info.** Combat resolution with fog-of-war views, paid spying with reveal/expiry, news feed with badge, free posting, and fake-news confidence shocks. _Goal: spy, strike, and manipulate._

**Phase 5 — Hidden objectives.** Objective deck, relative-threshold checks with hold duration, win detection, tells, and the end-game reveal. _Goal: someone can actually win._

**Phase 6 — Polish.** The full fancy theme, animations, balance passing on starting values and thresholds, reconnection handling, and optional SQLite if you want rooms to survive restarts.

---

If somebody attack someone the game itself will add to news that the player X attacked the player Y and the player Y will be notified in his notification which say this value in his currancy is lost from that attack and the figures and stackes will be updated

if news agency is destroyed the player will lose his news agency and will not be able to post news. and if he build another news agency he will be able to post news again. the damaged resources earn less mony. the damaged newsagency could be repaired by paying mony. the damged news agency gives you less permission to post news you should wait some min. there is a progress bar for each building and placeable that show the health of the building and the time to repair it. the progress bar will be updated in the map game screen. if the progress bar is completed it is ready to use news agency or it earn money notify and restart again. the same for resources and military buildings. the military buildings can be used to attack other players. the resources can be used to earn money. the news agency can be used to post news.

## 15. Build & run

```bash
go mod init geo-game
go get github.com/coder/websocket github.com/a-h/templ
go install github.com/a-h/templ/cmd/templ@latest

templ generate            # compile web/pages/*.templ -> *.go
go run ./cmd/server       # serves on :8080
```

Open `http://localhost:8080`, create a room by name, share the name with friends, and they join the same unlisted room — no password, no public list.
