package game

// The only win condition is assembling NukeWinCount nuclear weapons. Capital no
// longer wins — instead two capital thresholds set a nation's world tier.
const (
	NukeWinCount = 5

	// Default capital thresholds separating the world tiers.
	SecondWorldDefault = 3_000.0 // Third -> Second
	FirstWorldDefault  = 8_000.0 // Second -> First

	// A nuclear weapon consumes resources + cash to assemble.
	NukeOil     = 2
	NukeUranium = 1
	NukeCash    = 1_500.0
	NukeValue   = 2_000.0 // for combat cash-damage scaling

	// A First-World satellite reveals every rival's map (general-unit price).
	SatelliteCost = 2_500.0

	// Each production cycle a factory pays out this fraction of its build value.
	factoryPayoutRate = 0.3
)

// World development tiers, by capital against two thresholds:
//
//	third (0): < secondAt    second (1): secondAt..firstAt    first (2): >= firstAt
const (
	TierThird  = 0
	TierSecond = 1
	TierFirst  = 2
)

func worldTier(capital, secondAt, firstAt float64) int {
	switch {
	case capital >= firstAt:
		return TierFirst
	case capital >= secondAt:
		return TierSecond
	default:
		return TierThird
	}
}

func worldName(tier int) string {
	switch tier {
	case TierFirst:
		return "First World"
	case TierSecond:
		return "Second World"
	default:
		return "Third World"
	}
}

// FactoryDef is a buildable factory: its resource recipe (consumed on build),
// production cadence, and presentation. Build value and payout are derived from
// the recipe — pricier recipes earn more.
type FactoryDef struct {
	Key     string
	Title   string
	Icon    string
	Recipe  map[string]int
	Cycle   int // ticks between payouts
	MinTier int // lowest world tier allowed to build it
}

// Cycle is in ticks (500ms each), so these are roughly 10–16s between payouts.
// MinTier gates advanced industry behind economic development.
var factoryDefs = []FactoryDef{
	// Third-World tech (everyone)
	{"food", "Food Plant", "🍔", map[string]int{"oil": 1, "grain": 1, "water": 2}, 24, TierThird},
	{"sawmill", "Sawmill", "🪚", map[string]int{"oil": 1, "wood": 1}, 20, TierThird},
	{"beer", "Brewery", "🍺", map[string]int{"oil": 1, "grain": 2, "water": 1}, 24, TierThird},
	{"textile", "Textile Mill", "🧵", map[string]int{"oil": 1, "cotton": 1, "water": 1}, 24, TierThird},
	// Second-World tech
	{"refinery", "Refinery", "🏭", map[string]int{"oil": 1, "plastic": 2}, 28, TierSecond},
	{"glassworks", "Glassworks", "🏗️", map[string]int{"oil": 1, "coal": 1, "glass": 1}, 28, TierSecond},
	{"steel", "Steel Mill", "🔩", map[string]int{"oil": 1, "iron": 2, "coal": 1}, 30, TierSecond},
	// First-World tech
	{"car", "Car Plant", "🚗", map[string]int{"oil": 2, "iron": 1, "glass": 1, "plastic": 1}, 32, TierFirst},
	{"jewelry", "Jewelry", "💎", map[string]int{"oil": 1, "gold": 2, "iron": 1}, 28, TierFirst},
	{"nuclear", "Nuclear Plant", "⚛️", map[string]int{"oil": 1, "uranium": 1, "water": 1}, 30, TierFirst},
}

func factoryByKey(key string) (FactoryDef, bool) {
	for _, d := range factoryDefs {
		if d.Key == key {
			return d, true
		}
	}
	return FactoryDef{}, false
}

// buildValue is the resource cost of a factory at base prices.
func (d FactoryDef) buildValue() float64 {
	var v float64
	for r, n := range d.Recipe {
		v += float64(n) * basePrice(r)
	}
	return v
}

// payout is the cash a healthy factory produces each cycle.
func (d FactoryDef) payout() float64 { return d.buildValue() * factoryPayoutRate }

// buildFactory consumes the recipe from the player's stock and places a factory.
func (r *Room) buildFactory(p *Player, key string) {
	def, ok := factoryByKey(key)
	if !ok {
		r.errTo(p, errStr("unknown factory type"))
		return
	}
	if r.tierOf(p) < def.MinTier {
		r.errTo(p, errStr("your nation must be "+worldName(def.MinTier)+" to build a "+def.Title))
		return
	}
	var missing []string
	for res, n := range def.Recipe {
		if p.Resources[res] < n {
			missing = append(missing, itoa(n-p.Resources[res])+" "+res)
		}
	}
	if len(missing) > 0 {
		r.errTo(p, errStr("you still need "+joinComma(missing)+" to build a "+def.Title))
		return
	}
	for res, n := range def.Recipe {
		p.Resources[res] -= n
	}
	id := "fac-" + key + "-" + newID()[:4]
	gx, gy := resolvePlacement(p.Country.Placeables, "", 0.4, 0.45)
	pl := &Placeable{
		ID: id, Kind: KindFactory, Subtype: key, Icon: def.Icon,
		X: gx, Y: gy,
		HP: 100, MaxHP: 100, Value: def.buildValue(), Payout: def.payout(),
		Cooldown: def.Cycle, CooldownMax: def.Cycle,
	}
	p.Country.Placeables[id] = pl
	r.notice(p, "built a "+def.Title+" — it will start producing income shortly")
	r.sendState(p, "map.update")
}

// buildNuke assembles a nuclear weapon from oil + uranium + cash.
func (r *Room) buildNuke(p *Player) {
	if r.tierOf(p) < TierFirst {
		r.errTo(p, errStr("only a First-World nation can assemble nuclear weapons"))
		return
	}
	if p.Resources["oil"] < NukeOil || p.Resources["uranium"] < NukeUranium {
		r.errTo(p, errStr("a nuke needs "+itoa(NukeOil)+" oil and "+itoa(NukeUranium)+" uranium"))
		return
	}
	if p.Cash < NukeCash {
		r.errTo(p, errStr("a nuke needs "+fmtMoney(NukeCash)+" "+p.Country.Currency+" to assemble"))
		return
	}
	p.Resources["oil"] -= NukeOil
	p.Resources["uranium"] -= NukeUranium
	p.Cash -= NukeCash

	// Each nuke is a placeable on the map — attackable, and only counted while
	// it survives (a damaged one still counts; a destroyed one does not).
	id := "nuke-" + newID()[:4]
	gx, gy := resolvePlacement(p.Country.Placeables, "", 0.5, 0.62)
	p.Country.Placeables[id] = &Placeable{
		ID: id, Kind: KindNuke, Subtype: "nuke", Icon: "☢️",
		X: gx, Y: gy,
		HP: 100, MaxHP: 100, Value: NukeValue,
	}
	r.notice(p, "assembled a nuclear weapon ("+itoa(p.nukeCount())+"/"+itoa(r.Cfg.NukeWin)+")")
	r.sendState(p, "map.update")
}

// buildSatellite launches a First-World satellite that permanently reveals every
// rival's map (and lets you target them).
func (r *Room) buildSatellite(p *Player) {
	if r.tierOf(p) < TierFirst {
		r.errTo(p, errStr("only a First-World nation can launch a satellite"))
		return
	}
	if p.hasSatellite {
		r.errTo(p, errStr("you already have a satellite in orbit"))
		return
	}
	cost := p.convertCost(SatelliteCost)
	if p.Cash < cost {
		r.errTo(p, errStr("a satellite needs "+fmtMoney(cost)+" "+p.Country.Currency))
		return
	}
	p.Cash -= cost
	p.hasSatellite = true
	r.notice(p, "satellite launched — every rival's map is now visible")
	r.sendState(p, "tick")
}
