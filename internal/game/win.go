package game

// checkWin runs every tick once the game has started. The only win condition is
// assembling the required number of nuclear weapons; the first to reach it wins.
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
	}
}
