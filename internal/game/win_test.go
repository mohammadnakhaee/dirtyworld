package game

import "testing"

type fakeConn struct{ sent [][]byte }

func (f *fakeConn) Send(b []byte) { f.sent = append(f.sent, b) }
func (f *fakeConn) Close()        {}

func newTestPlayer(r *Room, name string) *Player {
	p := &Player{
		ID: newID(), Name: name, Country: GenerateCountry(1, name, name+" currency"),
		Cash: StartingCash, Resources: map[string]int{}, Confidence: 1,
	}
	p.addConn(&fakeConn{})
	r.Players[p.ID] = p
	r.order = append(r.order, p.ID)
	return p
}

// giveNukes adds n surviving nuke placeables to the player.
func giveNukes(p *Player, n int) {
	for i := 0; i < n; i++ {
		id := "nuke-test-" + itoa(i)
		p.Country.Placeables[id] = &Placeable{ID: id, Kind: KindNuke, HP: 100, MaxHP: 100}
	}
}

func TestNukeWin(t *testing.T) {
	r := NewRoom("t", Config{})
	p := newTestPlayer(r, "Veska")
	newTestPlayer(r, "Tyros")
	giveNukes(p, NukeWinCount)
	r.checkWin()
	if !r.over || r.WinnerID != p.ID {
		t.Fatalf("expected nuke win for %s, over=%v winner=%s", p.ID, r.over, r.WinnerID)
	}
}

func TestCapitalWin(t *testing.T) {
	r := NewRoom("t", Config{})
	p := newTestPlayer(r, "Veska")
	newTestPlayer(r, "Tyros")
	p.Cash = CapitalWinTarget + 1 // pure cash is capital
	r.checkWin()
	if !r.over || r.WinnerID != p.ID {
		t.Fatalf("expected capital win for %s, over=%v winner=%s", p.ID, r.over, r.WinnerID)
	}
}

func TestNoWinBelowThresholds(t *testing.T) {
	r := NewRoom("t", Config{})
	p := newTestPlayer(r, "Veska")
	giveNukes(p, NukeWinCount-1)
	p.Cash = CapitalWinTarget - 1
	r.checkWin()
	if r.over {
		t.Fatalf("did not expect a win: nukes=%d cash=%.0f", p.nukeCount(), p.Cash)
	}
}

func TestDestroyedNukeNotCounted(t *testing.T) {
	r := NewRoom("t", Config{})
	p := newTestPlayer(r, "Veska")
	newTestPlayer(r, "Tyros")
	giveNukes(p, NukeWinCount)
	// Destroy one nuke: now below the threshold, no win.
	for _, pl := range p.Country.Placeables {
		if pl.Kind == KindNuke {
			pl.HP = 0
			break
		}
	}
	if got := p.nukeCount(); got != NukeWinCount-1 {
		t.Fatalf("destroyed nuke still counted: got %d, want %d", got, NukeWinCount-1)
	}
	r.checkWin()
	if r.over {
		t.Fatalf("should not win with a destroyed nuke below threshold")
	}
}

func TestWorldTier(t *testing.T) {
	const target = 9000
	cases := []struct {
		cap  float64
		want int
	}{
		{0, TierThird}, {2999, TierThird},
		{3000, TierSecond}, {5999, TierSecond},
		{6000, TierFirst}, {12000, TierFirst},
	}
	for _, c := range cases {
		if got := worldTier(c.cap, target); got != c.want {
			t.Fatalf("worldTier(%.0f, %d) = %d, want %d", c.cap, target, got, c.want)
		}
	}
}

func TestStartingCapitalExcludesBuildings(t *testing.T) {
	r := NewRoom("t", Config{})
	p := newTestPlayer(r, "Veska")
	// Fresh player: only starting cash, no resources, no factories.
	if got := p.capital(r.Market); got != StartingCash {
		t.Fatalf("starting capital = %.0f, want %.0f (agency/military must not count)", got, StartingCash)
	}
}

func TestSatelliteNoHang(t *testing.T) {
	r := NewRoom("t", Config{CapitalTarget: 300})
	p := newTestPlayer(r, "Aaa")
	p.Cash = 5000 // capital 5000 >> 2/3*300 -> First World
	r.buildSatellite(p)
	if !p.hasSatellite {
		t.Fatal("satellite not set")
	}
}
