package game

// resolveAttack applies asymptotic damage: a small attack chips, an
// overwhelming one destroys. defense is the target placeable's own resilience.
func resolveAttack(spend, defense, hp float64) (newHP float64, destroyed bool) {
	ratio := spend / (defense + 1)
	damage := hp * (ratio / (ratio + 1)) // 0..hp, asymptotic
	newHP = hp - damage
	if newHP <= 1 {
		return 0, true
	}
	return newHP, false
}

// CombatResult is broadcast/notified after a strike.
type CombatResult struct {
	AttackerID   string
	AttackerName string
	DefenderID   string
	DefenderName string
	PlaceableID  string
	Subtype      string
	Damage       float64
	Destroyed    bool
	CashLost     float64
}

// doAttack validates and resolves an attack from p against a target placeable.
// The attacker commits cash as their military budget; damage scales the
// target's value into a cash loss for the defender.
func (r *Room) doAttack(attacker *Player, targetPlayerID, targetPlaceableID string, spend float64) (*CombatResult, error) {
	if spend <= 0 {
		return nil, errStr("attack budget must be positive")
	}
	if attacker.Cash < spend {
		return nil, errStr("not enough cash to fund the attack")
	}
	if targetPlayerID == attacker.ID {
		return nil, errStr("cannot attack yourself")
	}
	// You can only strike a country you currently have eyes on (spy or satellite).
	if !r.canSee(attacker.ID, targetPlayerID) {
		return nil, errStr("you can only attack a country you can see — spy on it or launch a satellite")
	}
	// Attacker must own a working military building to project force.
	if !attacker.hasReadyMilitary() {
		return nil, errStr("no ready military building to attack from")
	}
	def, ok := r.Players[targetPlayerID]
	if !ok {
		return nil, errStr("target player not found")
	}
	pl, ok := def.Country.Placeables[targetPlaceableID]
	if !ok || pl.destroyed() {
		return nil, errStr("target not found")
	}

	attacker.Cash -= spend
	// Defense resilience: military bases are hardened; nuclear silos somewhat.
	defense := 40.0
	switch pl.Kind {
	case KindMilitary:
		defense = 90.0
	case KindNuke:
		defense = 60.0
	}
	prevHP := pl.HP
	newHP, destroyed := resolveAttack(spend, defense, pl.HP)
	pl.HP = newHP
	dmg := prevHP - newHP

	// Cash damage to the defender, proportional to value lost.
	cashLost := pl.Value * (dmg / 100.0) * 0.4
	if cashLost > def.Cash {
		cashLost = def.Cash
	}
	def.Cash -= cashLost
	def.Confidence = clamp(def.Confidence-0.08, 0.5, 1.4) // morale hit

	// Fire the attacker's military cooldown.
	for _, m := range attacker.Country.Placeables {
		if m.Kind == KindMilitary && !m.destroyed() && m.Cooldown == 0 {
			m.Cooldown = m.CooldownMax
			break
		}
	}

	res := &CombatResult{
		AttackerID: attacker.ID, AttackerName: attacker.Name,
		DefenderID: def.ID, DefenderName: def.Name,
		PlaceableID: pl.ID, Subtype: pl.Subtype,
		Damage: dmg, Destroyed: destroyed, CashLost: cashLost,
	}
	return res, nil
}

func (p *Player) hasReadyMilitary() bool {
	for _, pl := range p.Country.Placeables {
		if pl.Kind == KindMilitary && !pl.destroyed() && pl.Cooldown == 0 {
			return true
		}
	}
	return false
}

type strErr string

func (e strErr) Error() string { return string(e) }
func errStr(s string) error    { return strErr(s) }
