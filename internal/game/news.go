package game

import "strings"

// canPost reports whether the player owns a working news agency. A destroyed
// agency silences them until they build or repair one.
func (p *Player) canPost() (*Placeable, bool) {
	for _, pl := range p.Country.Placeables {
		if pl.Kind == KindNewsAgency && !pl.destroyed() {
			return pl, true
		}
	}
	return nil, false
}

// publishNews appends a real item sourced to the player's own agency. Free, but
// a damaged agency posts on a cooldown (less "press freedom").
func (r *Room) publishNews(p *Player, headline, body string) (*NewsItem, error) {
	agency, ok := p.canPost()
	if !ok {
		return nil, errStr("you have no working news agency")
	}
	if agency.Cooldown > 0 {
		return nil, errStr("your damaged agency is still recovering — wait a moment")
	}
	headline = strings.TrimSpace(headline)
	if headline == "" {
		return nil, errStr("headline required")
	}
	item := &NewsItem{
		ID: newID(), SourceID: p.ID, SourceName: p.Country.Name,
		Headline: clip(headline, 120), Body: clip(strings.TrimSpace(body), 400),
		Tick: r.Tick,
	}
	r.appendNews(item)
	// Healthy agency: short cooldown. Damaged: longer (set on damage).
	agency.Cooldown = 2 + int((1-agency.healthFrac())*16)
	agency.CooldownMax = agency.Cooldown
	return item, nil
}

// hackNews plants a fake item whose source is the VICTIM's agency, and applies a
// confidence shock to the targeted country. Players never see the Fake flag.
func (r *Room) hackNews(attacker *Player, targetID, headline, body string) (*NewsItem, error) {
	if targetID == attacker.ID {
		return nil, errStr("hack a rival, not yourself")
	}
	victim, ok := r.Players[targetID]
	if !ok {
		return nil, errStr("target not found")
	}
	cost := attacker.convertCost(HackCost)
	if attacker.Cash < cost {
		return nil, errStr("not enough cash to run a disinformation op — need " + fmtMoney(cost) + " " + attacker.Country.Currency)
	}
	if _, ok := victim.canPost(); !ok {
		return nil, errStr("target has no agency to impersonate")
	}
	attacker.Cash -= cost
	item := &NewsItem{
		ID: newID(), SourceID: victim.ID, SourceName: victim.Country.Name,
		Headline: clip(strings.TrimSpace(headline), 120), Body: clip(strings.TrimSpace(body), 400),
		Fake: true, Tick: r.Tick,
	}
	r.appendNews(item)
	victim.Confidence = clamp(victim.Confidence-HackShock, 0.5, 1.4)
	r.notice(victim, "your currency confidence took a hit — a story is spreading")
	return item, nil
}

func (r *Room) appendNews(item *NewsItem) {
	r.News = append(r.News, item)
	const cap = 80
	if len(r.News) > cap {
		r.News = r.News[len(r.News)-cap:]
	}
}

// systemNews posts an item attributed to the wire service (e.g. attack reports)
// and pushes it to every player's feed.
func (r *Room) systemNews(headline string) *NewsItem {
	item := &NewsItem{
		ID: newID(), SourceID: "system", SourceName: "Global Wire",
		Headline: clip(headline, 140), Tick: r.Tick,
	}
	r.appendNews(item)
	r.broadcastNews(item)
	return item
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
