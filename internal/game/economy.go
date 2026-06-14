package game

import "math"

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// marketBaseDemand is the resting demand level; buying pushes a resource above
// it (price rises), and demand relaxes back over time.
const marketBaseDemand = 10.0

// priceUpdateTicks is how often prices recompute on their own — every 30s at the
// 500ms tick rate. Between these, prices only change when someone trades.
const priceUpdateTicks = 60

func NewMarket() *Market {
	m := &Market{Commodities: map[string]*Commodity{}}
	for _, d := range resourceDefs {
		m.Commodities[d.Name] = &Commodity{
			Name:     d.Name,
			BaseCost: d.Base,
			Demand:   marketBaseDemand,
			Price:    d.Base,
			History:  []float64{d.Base},
		}
	}
	return m
}

// convertCost turns a general-unit price into the player's own currency using
// their exchange rate (clamped 0.4..3.0, so never zero).
func (p *Player) convertCost(general float64) float64 {
	rate := p.Country.exchangeRate
	if rate <= 0 {
		rate = 1
	}
	return general / rate
}

func priceOf(m *Market, name string) float64 {
	if c, ok := m.Commodities[name]; ok {
		return c.Price
	}
	return basePrice(name)
}

// setPrice recomputes one resource's price from its current demand.
func setPrice(c *Commodity) {
	c.Price = c.BaseCost * clamp(c.Demand/marketBaseDemand, 0.5, 4)
}

// recordPrice appends the current price to the chart history.
func recordPrice(c *Commodity) {
	const histCap = 240
	c.History = append(c.History, c.Price)
	if len(c.History) > histCap {
		c.History = c.History[len(c.History)-histCap:]
	}
}

// recomputePrices is the periodic (30s) market correction: demand relaxes toward
// baseline so trade-driven spikes fade, then every price is recomputed/recorded.
func (m *Market) recomputePrices() {
	for _, c := range m.Commodities {
		c.Demand += (marketBaseDemand - c.Demand) * 0.4
		setPrice(c)
		recordPrice(c)
	}
}

// applyFactories advances each factory's production cycle; when a cycle
// completes it pays the owner cash (scaled by the factory's health). Other
// buildings just tick down any cooldown (e.g. military after firing).
func (r *Room) applyFactories() {
	for _, p := range r.Players {
		for _, pl := range p.Country.Placeables {
			if pl.destroyed() {
				continue
			}
			if pl.Cooldown > 0 {
				pl.Cooldown--
				continue
			}
			if pl.Kind == KindFactory {
				pay := pl.Payout * pl.healthFrac()
				if pay > 0 {
					p.Cash += pay
					r.notice(p, "your "+factoryTitle(pl.Subtype)+" produced "+fmtMoney(pay)+" "+p.Country.Currency)
				}
				pl.Cooldown = pl.CooldownMax
			}
		}
	}
}

func factoryTitle(key string) string {
	if d, ok := factoryByKey(key); ok {
		return d.Title
	}
	return key
}

// recomputeCurrencies applies E = (K / Kref) × C × M and decays confidence.
// Kref blends the field average with the starting capital, so a currency
// strengthens with a nation's own economic growth (visible even in a solo game)
// as well as relative to rivals.
func (r *Room) recomputeCurrencies() {
	if len(r.Players) == 0 {
		return
	}
	var total float64
	caps := make(map[string]float64, len(r.Players))
	for id, p := range r.Players {
		// Use gross (un-converted) capital so the rate doesn't depend on itself.
		k := p.grossCapital(r.Market)
		caps[id] = k
		total += k
	}
	avg := total / float64(len(r.Players))
	ref := (avg + r.Cfg.StartingCash) / 2 // anchor so growth (not just rank) moves the rate
	if ref <= 0 {
		ref = StartingCash
	}
	for id, p := range r.Players {
		k := caps[id]
		base := k / ref
		m := 1.0
		if p.PrevCapital > 0 {
			m = 1 + 0.3*((k-p.PrevCapital)/p.PrevCapital)
		}
		rate := base * p.Confidence * clamp(m, 0.7, 1.4)
		p.Country.exchangeRate = clamp(rate, 0.4, 3.0)
		p.PrevCapital = k
		p.Confidence += (1.0 - p.Confidence) * 0.05
	}
}

func fmtMoney(v float64) string {
	return strconvFloat(math.Round(v))
}
