# Dirty World — geo-economic strategy game

A 5+ player multiplayer browser game: each player runs a country with cash, a
currency, resources, a military, and a news agency. Everyone has a **hidden
objective** and races silently to complete it — the first to declare a satisfied
objective wins instantly. Counterplay happens before completion, driven by public
"tells" (a hot currency, a price spike, a troop buildup, a hacked headline).

Built with **Go**, server-side-rendered entry pages, and a single authoritative
**WebSocket** game stream. No build step beyond `go run`.

## Run

The full stack (Go server behind nginx) on plain **http://localhost** via Docker:

```bash
make server      # docker compose up --build  → http://localhost
make down        # stop the stack
```

Or run the Go server directly for local development (no Docker):

```bash
go run ./cmd/server      # serves http://localhost:8080
```

### Make targets

| Command | What it does |
| --- | --- |
| `make server` | Build & run the stack (Go app + nginx) → **http://localhost** |
| `make down` | Stop the stack and remove its containers |
| `make build` | Compile a binary to `bin/server` |
| `make test` | Run the test suite |
| `make fmt` | Format Go sources |
| `make vet` | Static checks |

`make server` uses Docker Compose (`docker-compose.yml` + `nginx.conf`); the
other targets use your local Go toolchain. Publishing port 80 goes through the
Docker daemon, so you need permission to run `docker` (be in the `docker` group
or use `sudo`).

Open <http://localhost> (or <http://localhost:8080> for the direct run), pick a **room name** and your **country name**, and
share the room name with friends — there is no public list and no password, so a
name you don't share is effectively private. Your currency is named after your
country (e.g. country "Veska" → "Veska currency").

The creator is the **host**: everyone who joins lands in a waiting room, and the
host presses **Start** once the players are in. The economy and objectives only
begin at that point. If you drop mid-game, rejoin the same room with the **same
country name** to resume your seat exactly where you left it.

## How it plays

- **Economy.** Commodity price `P = (D / S) × C`. Buying corners supply and spikes
  the price (a visible tell); production feeds supply and pays a currency dividend.
- **Currency.** Exchange rate `E = (K / K̄) × C × M` — relative capital × confidence
  × momentum. The currency board ranks every nation's rate live.
- **Information war.** Publish to your own agency for free; pay to plant **fake
  news** sourced as a rival — it shocks their confidence and devalues their
  currency. Destroy an agency and its owner can't post until they rebuild it.
- **Conflict & fog-of-war.** You see *that* a rival's buildings exist and rough HP,
  but not their value — buy **espionage** to reveal the truth for a while. Attacks
  commit a budget against a target's defense with asymptotic damage.
- **Buildings.** Resources, military, and agencies each have an HP ring and a
  cooldown/repair ring on the map. Damaged buildings produce less and recover
  slower; repair them with cash.
- **Hidden objectives.** Hegemon, Monopolist, Hard Currency, Conqueror, Arms
  Dealer, Cartel Boss — each a relative threshold held for N consecutive ticks.
  One tick before the hold completes, every rival gets a "someone is close" tell.

## Architecture

```
cmd/server         HTTP router, SSR pages, WS endpoint, static files
internal/proto     WebSocket message envelope + payload types
internal/hub       room registry + per-connection WebSocket pumps
internal/game      authoritative state & rules; one goroutine per room owns it
web/templates      landing + game shell (html/template)
web/static         CSS theme + JS (client / map / charts / news)
```

**Concurrency model:** each room runs in a single goroutine that owns all of its
state. Connections feed *commands* in over a channel; the room broadcasts
fog-of-war-filtered *snapshots/events* back out. The only shared structure is the
hub's `map[string]*Room`, guarded by a mutex. Verified clean under `go run -race`.

The server is authoritative: clients send **intent** (`buy 10 oil`), never money,
prices, or damage. The server computes results against true state and strips
hidden values per recipient before sending.
