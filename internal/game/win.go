package game

// checkWin runs every tick once the game has started. The win rules are public;
// the first player to satisfy either of them wins automatically — there is no
// secret objective and no declare step.
func (r *Room) checkWin() {
	for _, id := range r.order { // deterministic order
		p := r.Players[id]
		if p == nil {
			continue
		}
		if p.nukeCount() >= r.Cfg.NukeWin {
			r.win(p, "assembled "+itoa(r.Cfg.NukeWin)+" nuclear weapons")
			return
		}
		if p.capital(r.Market) >= r.Cfg.CapitalTarget {
			r.win(p, "amassed "+fmtMoney(r.Cfg.CapitalTarget)+" in capital")
			return
		}
	}
}
