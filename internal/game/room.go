package game

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"geo-game/internal/proto"
)

// NewRoom constructs a room and its channels. Call Run in a goroutine.
func NewRoom(name string, cfg Config) *Room {
	return &Room{
		Name:       name,
		Cfg:        cfg.withDefaults(),
		Players:    map[string]*Player{},
		byConn:     map[Conn]*Player{},
		Market:     NewMarket(),
		commands:   make(chan Command, 256),
		register:   make(chan *JoinReq, 16),
		unregister: make(chan Conn, 16),
		done:       make(chan struct{}),
	}
}

// Channel accessors for the hub (which lives in another package).
func (r *Room) Register(j *JoinReq) { r.register <- j }
func (r *Room) Unregister(c Conn)   { r.unregister <- c }
func (r *Room) Submit(c Command)    { r.commands <- c }

// Run is the single goroutine that owns all room state. Commands, joins,
// leaves and ticks are serialized by this select — no locks in game logic.
func (r *Room) Run() {
	ticker := time.NewTicker(TickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.done:
			return
		case j := <-r.register:
			r.addPlayer(j)
		case c := <-r.unregister:
			r.removePlayer(c)
		case cmd := <-r.commands:
			r.handleCommand(cmd)
		case <-ticker.C:
			r.tick()
		}
	}
}

func (r *Room) tick() {
	r.Tick++
	// The economy only runs once the host has started the match; before that
	// the room is a lobby and we just keep clients refreshed.
	if r.Started && !r.over {
		r.applyFactories()
		// Prices update on a slow cadence (every 30s); trades update them
		// immediately in executeOrder.
		if r.Tick%priceUpdateTicks == 0 {
			r.Market.recomputePrices()
		}
		r.recomputeCurrencies()
		r.checkWin()
	}
	if r.gc() {
		return // room was torn down
	}
	r.broadcastState(proto.EvtTick)
}

// GraceTicks is how long a seat (and the room) is held after a player's last
// connection drops, so same-tab navigation (map <-> console) can reconnect
// without losing the seat or dropping the room. 10 ticks ≈ 5s.
const GraceTicks = 10

// gc holds disconnected seats for a grace window, then drops lobby leavers and
// tears the room down once everyone is gone. Returns true if the room shut down.
func (r *Room) gc() bool {
	for _, id := range append([]string{}, r.order...) {
		p := r.Players[id]
		if p == nil || p.connected() || r.Tick-p.dcTick < GraceTicks {
			continue
		}
		if r.Started {
			continue // started game: hold the seat for a later rejoin by name
		}
		r.dropPlayer(p)
	}
	if r.connectedCount() == 0 && r.graceElapsed() {
		r.shutdown()
		return true
	}
	return false
}

// graceElapsed reports whether enough time has passed since the most recent
// disconnect to safely tear the room down.
func (r *Room) graceElapsed() bool {
	newest := -1 << 30
	for _, p := range r.Players {
		if p.dcTick > newest {
			newest = p.dcTick
		}
	}
	return len(r.Players) == 0 || r.Tick-newest >= GraceTicks
}

// dropPlayer removes a player from the lobby and reassigns the host if needed.
func (r *Room) dropPlayer(p *Player) {
	delete(r.Players, p.ID)
	for i, id := range r.order {
		if id == p.ID {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
	if r.HostID == p.ID {
		r.HostID = ""
		if len(r.order) > 0 {
			r.HostID = r.order[0]
		}
	}
	r.systemNews(p.Country.Name + " has left the lobby.")
}

// ---- player lifecycle ----

func (r *Room) addPlayer(j *JoinReq) {
	// A player already owns this country name.
	if existing := r.playerByCountry(j.CountryName); existing != nil {
		switch {
		case existing.Token != "" && existing.Token == j.Token:
			// Same browser session opening another view (e.g. the console tab).
			existing.addConn(j.Conn)
			r.byConn[j.Conn] = existing
			j.reply(nil)
			r.sendState(existing, proto.EvtSnapshot)
			return
		case existing.connected():
			// A different session trying to take a name that's in use.
			j.reply(errStr("that country name is already in play — pick another"))
			return
		default:
			// The seat was dropped/held — resume it under this new session.
			existing.Token = j.Token
			existing.addConn(j.Conn)
			r.byConn[j.Conn] = existing
			j.reply(nil)
			r.systemNews(existing.Country.Name + " has reconnected.")
			r.sendState(existing, proto.EvtSnapshot)
			r.broadcastState(proto.EvtTick)
			return
		}
	}

	// No match: a brand-new player. Only allowed before the game starts.
	if r.Started {
		j.reply(errStr("the game has already started — rejoin with the country name you played"))
		return
	}
	if len(r.Players) >= MaxPlayers {
		j.reply(errStr("room is full"))
		return
	}

	seed := time.Now().UnixNano() + int64(len(r.Players)*7919)
	currency := j.CountryName + " currency"
	country := GenerateCountry(seed, j.CountryName, currency)
	p := &Player{
		ID: newID(), Name: j.CountryName, Token: j.Token, Country: country,
		Cash: r.Cfg.StartingCash, Resources: map[string]int{},
		Confidence: 1.0, PrevCapital: r.Cfg.StartingCash,
		SpyAccess: map[string]int{},
	}
	p.addConn(j.Conn)
	r.Players[p.ID] = p
	r.order = append(r.order, p.ID)
	r.byConn[j.Conn] = p
	if r.HostID == "" {
		r.HostID = p.ID // first to join controls the lobby
	}

	j.reply(nil)
	r.systemNews(country.Name + " has entered the lobby.")
	r.sendState(p, proto.EvtSnapshot)
	r.broadcastState(proto.EvtTick)
}

// closeRoom lets the host tear the whole room down: every player is told the
// room closed, all connections are dropped, and the room is removed from the hub
// so it can't be rejoined.
func (r *Room) closeRoom(p *Player) {
	if p.ID != r.HostID {
		r.errTo(p, errStr("only the host can close the room"))
		return
	}
	for _, pl := range r.Players {
		r.emit(pl, proto.EvtRoomClosed, map[string]string{"message": "The host closed the room."})
	}
	for c := range r.byConn {
		c.Close()
	}
	r.shutdown()
}

// startGame is invoked by the host to leave the lobby and begin play.
func (r *Room) startGame(p *Player) {
	if p.ID != r.HostID {
		r.errTo(p, errStr("only the host can start the game"))
		return
	}
	if r.Started {
		return
	}
	r.Started = true
	r.systemNews("The game has begun. First to " + itoa(r.Cfg.NukeWin) +
		" nuclear weapons or " + fmtMoney(r.Cfg.CapitalTarget) + " capital wins.")
	r.broadcastState(proto.EvtTick)
}

func (r *Room) removePlayer(c Conn) {
	p, ok := r.byConn[c]
	if !ok {
		return
	}
	delete(r.byConn, c)
	p.dropConn(c)
	if p.connected() {
		return // this player still has another view open
	}
	// Last view closed. Don't remove the seat or shut the room down yet — a grace
	// window (see gc) lets same-tab navigation reconnect without dropping out.
	p.dcTick = r.Tick
	r.broadcastState(proto.EvtTick)
}

func (r *Room) shutdown() {
	if r.OnEmpty != nil {
		r.OnEmpty(r.Name)
	}
	close(r.done)
}

func (r *Room) connectedCount() int {
	n := 0
	for _, p := range r.Players {
		if p.connected() {
			n++
		}
	}
	return n
}

func (r *Room) playerByCountry(name string) *Player {
	for _, p := range r.Players {
		if strings.EqualFold(p.Country.Name, name) {
			return p
		}
	}
	return nil
}

// ---- command dispatch ----

func (r *Room) handleCommand(cmd Command) {
	p, ok := r.byConn[cmd.Conn]
	if !ok {
		return
	}
	// The host can close the room at any time — including after a win.
	if cmd.Type == proto.CmdCloseRoom {
		r.closeRoom(p)
		return
	}
	if r.over {
		return
	}
	if cmd.Type == proto.CmdStartGame {
		r.startGame(p)
		return
	}
	// In the lobby, only arranging your own buildings is allowed.
	if !r.Started && cmd.Type != proto.CmdPlaceableMove {
		r.errTo(p, errStr("the game hasn't started yet"))
		return
	}
	switch cmd.Type {
	case proto.CmdPlaceableMove:
		var m proto.PlaceableMove
		if json.Unmarshal(cmd.Raw, &m) == nil {
			r.movePlaceable(p, m)
		}
	case proto.CmdMarketOrder:
		var m proto.MarketOrder
		if json.Unmarshal(cmd.Raw, &m) == nil {
			if err := r.executeOrder(p, m.Commodity, m.Side, m.Qty); err != nil {
				r.errTo(p, err)
			} else {
				r.sendState(p, proto.EvtTick)
			}
		}
	case proto.CmdBuyService:
		var m proto.BuyService
		if json.Unmarshal(cmd.Raw, &m) == nil {
			r.buyService(p, m)
		}
	case proto.CmdBuildFactory:
		var m proto.BuildFactory
		if json.Unmarshal(cmd.Raw, &m) == nil {
			r.buildFactory(p, m.Type)
		}
	case proto.CmdBuildNuke:
		r.buildNuke(p)
	case proto.CmdRepair:
		var m proto.Repair
		if json.Unmarshal(cmd.Raw, &m) == nil {
			r.repair(p, m.ID)
		}
	case proto.CmdAttack:
		var m proto.Attack
		if json.Unmarshal(cmd.Raw, &m) == nil {
			r.handleAttack(p, m)
		}
	case proto.CmdSpy:
		var m proto.Spy
		if json.Unmarshal(cmd.Raw, &m) == nil {
			if err := r.buySpy(p, m.TargetPlayer); err != nil {
				r.errTo(p, err)
			} else {
				r.sendState(p, proto.EvtTick)
			}
		}
	case proto.CmdPublishNews:
		var m proto.PublishNews
		if json.Unmarshal(cmd.Raw, &m) == nil {
			if item, err := r.publishNews(p, m.Headline, m.Body); err != nil {
				r.errTo(p, err)
			} else {
				r.broadcastNews(item)
			}
		}
	case proto.CmdHackNews:
		var m proto.HackNews
		if json.Unmarshal(cmd.Raw, &m) == nil {
			if item, err := r.hackNews(p, m.TargetPlayer, m.Headline, m.Body); err != nil {
				r.errTo(p, err)
			} else {
				r.broadcastNews(item)
				r.sendState(p, proto.EvtTick)
			}
		}
	}
}

func (r *Room) movePlaceable(p *Player, m proto.PlaceableMove) {
	pl, ok := p.Country.Placeables[m.ID]
	if !ok {
		return
	}
	pl.X = clamp(m.X, 0, 1)
	pl.Y = clamp(m.Y, 0, 1)
	r.sendState(p, proto.EvtMapUpdate)
}

func (r *Room) buyService(p *Player, m proto.BuyService) {
	var general float64
	switch PlaceableKind(m.Kind) {
	case KindNewsAgency:
		general = CostAgency
	case KindMilitary:
		general = CostMilitary
	default:
		r.errTo(p, errStr("you can only buy a military base or news agency with cash"))
		return
	}
	cost := p.convertCost(general)
	if p.Cash < cost {
		r.errTo(p, errStr("not enough cash — need "+fmtMoney(cost)+" "+p.Country.Currency))
		return
	}
	p.Cash -= cost
	id := string(m.Kind[:3]) + "-" + newID()[:4]
	pl := newPlaceable(id, m.Kind, m.Subtype)
	// Place near the centre with a small offset so it doesn't fully overlap.
	pl.X = 0.45 + float64(len(p.Country.Placeables)%4)*0.04
	pl.Y = 0.45 + float64(len(p.Country.Placeables)%3)*0.05
	p.Country.Placeables[id] = pl
	r.notice(p, "built a new "+m.Kind+" for "+fmtMoney(cost)+" "+p.Country.Currency)
	r.sendState(p, proto.EvtMapUpdate)
}

func (r *Room) repair(p *Player, id string) {
	pl, ok := p.Country.Placeables[id]
	if !ok {
		return
	}
	missing := pl.MaxHP - pl.HP
	if missing <= 0 {
		r.errTo(p, errStr("already at full health"))
		return
	}
	cost := missing * RepairCostPerHP
	if p.Cash < cost {
		// Partial repair with whatever cash is available.
		affordable := p.Cash / RepairCostPerHP
		if affordable <= 0 {
			r.errTo(p, errStr("not enough cash to repair"))
			return
		}
		missing = affordable
		cost = missing * RepairCostPerHP
	}
	p.Cash -= cost
	pl.HP = clamp(pl.HP+missing, 0, pl.MaxHP)
	if pl.HP >= pl.MaxHP {
		pl.Cooldown = 0 // fully repaired buildings come back online
	}
	r.notice(p, "repaired "+pl.Subtype+" for "+fmtMoney(cost)+" "+p.Country.Currency)
	r.sendState(p, proto.EvtMapUpdate)
}

func (r *Room) handleAttack(p *Player, m proto.Attack) {
	res, err := r.doAttack(p, m.TargetPlayer, m.TargetPlaceable, m.Spend)
	if err != nil {
		r.errTo(p, err)
		return
	}
	// Public wire report + targeted notices.
	verb := "struck"
	if res.Destroyed {
		verb = "destroyed a " + res.Subtype + " in"
	}
	r.systemNews(res.AttackerName + " " + verb + " " + res.DefenderName)

	def := r.Players[res.DefenderID]
	if def != nil {
		r.notice(def, res.AttackerName+" attacked your "+res.Subtype+
			" — lost "+fmtMoney(res.CashLost)+" "+def.Country.Currency)
		// If the agency was hit, lengthen its recovery cooldown.
		if pl, ok := def.Country.Placeables[res.PlaceableID]; ok && pl.Kind == KindNewsAgency && !pl.destroyed() {
			pl.Cooldown = pl.CooldownMax + int((1-pl.healthFrac())*16)
		}
		r.sendCombat(def, res)
	}
	r.notice(p, "attacked "+res.DefenderName+"'s "+res.Subtype+
		" for "+fmtMoney(res.Damage)+" damage")
	r.sendCombat(p, res)
	r.broadcastState(proto.EvtTick)
}

// ---- win ----

func (r *Room) win(winner *Player, reason string) {
	r.over = true
	r.WinnerID = winner.ID
	standings := make([]map[string]any, 0, len(r.Players))
	for _, id := range r.order {
		p := r.Players[id]
		if p == nil {
			continue
		}
		standings = append(standings, map[string]any{
			"name":    p.Country.Name,
			"capital": round(p.capital(r.Market), 1),
			"nukes":   p.nukeCount(),
			"won":     p.ID == winner.ID,
		})
	}
	payload := map[string]any{
		"winnerId":   winner.ID,
		"winnerName": winner.Country.Name,
		"reason":     reason,
		"standings":  standings,
	}
	for _, p := range r.Players {
		r.emit(p, proto.EvtWin, payload)
	}
	r.systemNews(winner.Country.Name + " wins — " + reason + ".")
}

// ---- serialization / fog-of-war ----

func (r *Room) buildStateFor(p *Player) StateMsg {
	// Own country (full).
	own := CountryDTO{
		PlayerID: p.ID, Name: p.Country.Name, Currency: p.Country.Currency,
		Palette: p.Country.Palette, Boundary: p.Country.Boundary,
		ExchangeRate: p.Country.exchangeRate,
	}
	for _, pl := range p.Country.Placeables {
		own.Placeables = append(own.Placeables, ownPlaceableDTO(pl))
	}
	sortPlaceables(own.Placeables)

	// Rivals (fogged unless spied).
	var rivals []RivalDTO
	for _, id := range r.order {
		other := r.Players[id]
		if other == nil || other.ID == p.ID {
			continue
		}
		spied := r.spyActive(p.ID, other.ID)
		rd := RivalDTO{Spied: spied}
		rd.PlayerID = other.ID
		rd.Name = other.Country.Name
		rd.Currency = other.Country.Currency
		rd.Palette = other.Country.Palette
		rd.ExchangeRate = other.Country.exchangeRate
		// A rival's map (and stockpile) is only visible while you spy them.
		if spied {
			rd.Boundary = other.Country.Boundary
			for _, pl := range other.Country.Placeables {
				rd.Placeables = append(rd.Placeables, rivalPlaceableDTO(pl, true))
			}
			sortPlaceables(rd.Placeables)
			rd.Resources = cloneResources(other.Resources)
			n := other.nukeCount()
			rd.Nukes = &n
		}
		rivals = append(rivals, rd)
	}

	// Market — every resource, in catalog order.
	market := make([]CommodityDTO, 0, len(r.Market.Commodities))
	for _, name := range resourceNames() {
		c := r.Market.Commodities[name]
		if c == nil {
			continue
		}
		hist := make([]float64, len(c.History))
		copy(hist, c.History)
		market = append(market, CommodityDTO{Name: c.Name, Price: round(c.Price, 0.01), History: hist})
	}

	// Public currency board.
	board := make([]BoardEntry, 0, len(r.order))
	for _, id := range r.order {
		o := r.Players[id]
		if o == nil {
			continue
		}
		board = append(board, BoardEntry{
			PlayerID: o.ID, Name: o.Country.Name, Currency: o.Country.Currency,
			Palette: o.Country.Palette, ExchangeRate: round(o.Country.exchangeRate, 0.001),
			Self: o.ID == p.ID, Host: o.ID == r.HostID, Connected: o.connected(),
		})
	}
	sort.Slice(board, func(i, j int) bool { return board[i].ExchangeRate > board[j].ExchangeRate })

	_, canPost := p.canPost()
	self := SelfDTO{
		PlayerID: p.ID, Name: p.Name, Cash: round(p.Cash, 0.01),
		Confidence: round(p.Confidence, 0.001), Capital: round(p.capital(r.Market), 0.01),
		Resources: cloneResources(p.Resources), Nukes: p.nukeCount(),
		CanPost: canPost, IsHost: p.ID == r.HostID,
	}

	// Per-room win thresholds override the static catalog defaults.
	cat := gameCatalog
	cat.NukeTarget = r.Cfg.NukeWin
	cat.CapitalTarget = r.Cfg.CapitalTarget

	return StateMsg{
		Tick: r.Tick, Started: r.Started, Over: r.over, PlayerCount: len(r.Players),
		Self: self, Country: own, Rivals: rivals, Market: market, Board: board,
		News: r.News, Catalog: cat,
	}
}

func (r *Room) sendState(p *Player, evt string) {
	r.emit(p, evt, r.buildStateFor(p))
}

func (r *Room) broadcastState(evt string) {
	for _, p := range r.Players {
		r.sendState(p, evt)
	}
}

func (r *Room) broadcastNews(item *NewsItem) {
	for _, p := range r.Players {
		r.emit(p, proto.EvtNews, item)
	}
}

func (r *Room) sendCombat(p *Player, res *CombatResult) {
	r.emit(p, proto.EvtCombat, map[string]any{
		"attacker": res.AttackerName, "defender": res.DefenderName,
		"subtype": res.Subtype, "damage": round(res.Damage, 0.1),
		"destroyed": res.Destroyed, "cashLost": round(res.CashLost, 0.01),
	})
}

func (r *Room) notice(p *Player, text string) {
	r.emit(p, proto.EvtNotice, map[string]string{"text": text})
}

func (r *Room) errTo(p *Player, err error) {
	r.emit(p, proto.EvtError, map[string]string{"error": err.Error()})
}

// emit marshals payload once and pushes it to every connection (view) the
// player currently has open — map tab, console tab, etc.
func (r *Room) emit(p *Player, typ string, payload any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	env, err := json.Marshal(proto.Envelope{Type: typ, Payload: raw})
	if err != nil {
		return
	}
	for c := range p.conns {
		c.Send(env)
	}
}

func sortPlaceables(ps []PlaceableDTO) {
	sort.Slice(ps, func(i, j int) bool { return ps[i].ID < ps[j].ID })
}

// cloneResources copies a resource stockpile, keeping zero entries out.
func cloneResources(res map[string]int) map[string]int {
	out := map[string]int{}
	for k, v := range res {
		if v > 0 {
			out[k] = v
		}
	}
	return out
}
