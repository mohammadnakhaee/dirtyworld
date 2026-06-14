package game

import "errors"

// executeOrder buys or sells whole units of a resource at the current market
// price. Buying adds to the player's stockpile and pushes demand (price) up;
// selling returns cash and relaxes demand. The server owns the price.
func (r *Room) executeOrder(p *Player, resource, side string, qty float64) error {
	c, ok := r.Market.Commodities[resource]
	if !ok {
		return errors.New("unknown resource")
	}
	n := int(qty)
	if n <= 0 {
		return errors.New("quantity must be a positive whole number")
	}
	if p.Resources == nil {
		p.Resources = map[string]int{}
	}
	switch side {
	case "buy":
		// World price converted to the player's currency by exchange rate, the
		// same way Industry-card service prices are calculated.
		cost := p.convertCost(float64(n) * c.Price)
		if p.Cash < cost {
			return errors.New("not enough cash")
		}
		p.Cash -= cost
		p.Resources[resource] += n
		c.Demand += float64(n)
		r.notice(p, "bought "+itoa(n)+" "+resource+" for "+fmtMoney(cost)+" "+p.Country.Currency)
	case "sell":
		if p.Resources[resource] < n {
			return errors.New("you don't have that many " + resource)
		}
		gain := p.convertCost(float64(n) * c.Price)
		p.Resources[resource] -= n
		p.Cash += gain
		c.Demand -= float64(n) * 0.5
		if c.Demand < marketBaseDemand*0.2 {
			c.Demand = marketBaseDemand * 0.2
		}
		r.notice(p, "sold "+itoa(n)+" "+resource+" for "+fmtMoney(gain)+" "+p.Country.Currency)
	default:
		return errors.New("side must be buy or sell")
	}
	// A trade moves the price immediately (the other update cadence is 30s).
	setPrice(c)
	recordPrice(c)
	return nil
}
