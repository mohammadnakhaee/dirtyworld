package game

// Win conditions (public rules; players choose which to pursue):
//   - assemble NukeWinCount nuclear weapons, OR
//   - reach CapitalWinTarget in total capital.
const (
	NukeWinCount     = 5
	CapitalWinTarget = 10_000.0

	// A nuclear weapon consumes resources + cash to assemble.
	NukeOil     = 2
	NukeUranium = 1
	NukeCash    = 1_500.0
	NukeValue   = 2_000.0 // for combat cash-damage scaling

	// Each production cycle a factory pays out this fraction of its build value.
	factoryPayoutRate = 0.3
)

// FactoryDef is a buildable factory: its resource recipe (consumed on build),
// production cadence, and presentation. Build value and payout are derived from
// the recipe — pricier recipes earn more.
type FactoryDef struct {
	Key    string
	Title  string
	Icon   string
	Recipe map[string]int
	Cycle  int // ticks between payouts
}

// Cycle is in ticks (500ms each), so these are roughly 10–16s between payouts.
var factoryDefs = []FactoryDef{
	{"jewelry", "Jewelry", "💎", map[string]int{"oil": 1, "gold": 2, "iron": 1}, 28},
	{"beer", "Brewery", "🍺", map[string]int{"oil": 1, "grain": 2, "water": 1}, 24},
	{"car", "Car Plant", "🚗", map[string]int{"oil": 2, "iron": 1, "glass": 1, "plastic": 1}, 32},
	{"food", "Food Plant", "🍔", map[string]int{"oil": 1, "grain": 1, "water": 2}, 24},
	{"refinery", "Refinery", "🏭", map[string]int{"oil": 1, "plastic": 2}, 28},
	{"steel", "Steel Mill", "🔩", map[string]int{"oil": 1, "iron": 2, "coal": 1}, 30},
	{"sawmill", "Sawmill", "🪚", map[string]int{"oil": 1, "wood": 1}, 20},
	{"glassworks", "Glassworks", "🏗️", map[string]int{"oil": 1, "coal": 1, "glass": 1}, 28},
	{"textile", "Textile Mill", "🧵", map[string]int{"oil": 1, "cotton": 1, "water": 1}, 24},
	{"nuclear", "Nuclear Plant", "⚛️", map[string]int{"oil": 1, "uranium": 1, "water": 1}, 30},
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
	pl := &Placeable{
		ID: id, Kind: KindFactory, Subtype: key, Icon: def.Icon,
		X:  0.4 + float64(len(p.Country.Placeables)%4)*0.06,
		Y:  0.4 + float64(len(p.Country.Placeables)%3)*0.07,
		HP: 100, MaxHP: 100, Value: def.buildValue(), Payout: def.payout(),
		Cooldown: def.Cycle, CooldownMax: def.Cycle,
	}
	p.Country.Placeables[id] = pl
	r.notice(p, "built a "+def.Title+" — it will start producing income shortly")
	r.sendState(p, "map.update")
}

// buildNuke assembles a nuclear weapon from oil + uranium + cash.
func (r *Room) buildNuke(p *Player) {
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
	p.Country.Placeables[id] = &Placeable{
		ID: id, Kind: KindNuke, Subtype: "nuke", Icon: "☢️",
		X:  0.5 + float64(len(p.Country.Placeables)%5)*0.05,
		Y:  0.62 + float64(len(p.Country.Placeables)%3)*0.06,
		HP: 100, MaxHP: 100, Value: NukeValue,
	}
	r.notice(p, "assembled a nuclear weapon ("+itoa(p.nukeCount())+"/"+itoa(r.Cfg.NukeWin)+")")
	r.sendState(p, "map.update")
}
