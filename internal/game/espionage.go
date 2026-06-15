package game

// buySpy grants the buyer permanent reveal access to a rival's true map, values,
// and stockpile. Once bought it never expires.
func (r *Room) buySpy(buyer *Player, targetID string) error {
	if targetID == buyer.ID {
		return errStr("cannot spy on yourself")
	}
	if r.tierOf(buyer) < TierSecond {
		return errStr("your nation must be at least Second World to run espionage")
	}
	target, ok := r.Players[targetID]
	if !ok {
		return errStr("target player not found")
	}
	if buyer.SpyAccess == nil {
		buyer.SpyAccess = map[string]int{}
	}
	if _, already := buyer.SpyAccess[targetID]; already {
		return errStr("you already have permanent eyes on " + target.Name)
	}
	cost := buyer.convertCost(SpyCost)
	if buyer.Cash < cost {
		return errStr("not enough cash to fund espionage — need " + fmtMoney(cost) + " " + buyer.Country.Currency)
	}
	buyer.Cash -= cost
	buyer.SpyAccess[targetID] = r.Tick // recorded; presence = permanent access
	r.notice(buyer, "espionage established on "+target.Name+" — their map is now permanently visible")
	return nil
}

// spyActive reports whether buyer has ever established espionage on target.
func (r *Room) spyActive(buyerID, targetID string) bool {
	p := r.Players[buyerID]
	if p == nil || p.SpyAccess == nil {
		return false
	}
	_, ok := p.SpyAccess[targetID]
	return ok
}
