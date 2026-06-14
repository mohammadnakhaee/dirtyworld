package game

import (
	"math"
	"math/rand"
)

var palettes = []string{
	"#5eead4", "#a78bfa", "#f472b6", "#fbbf24",
	"#60a5fa", "#34d399", "#fb7185", "#c084fc",
	"#22d3ee", "#f59e0b", "#4ade80", "#e879f9",
}

// GenerateCountry builds a procedural country from a seed. Players start with no
// resources and no factories — only a news agency and a military base.
func GenerateCountry(seed int64, name, currency string) *Country {
	rng := rand.New(rand.NewSource(seed))
	c := &Country{
		Name:         name,
		Currency:     currency,
		Seed:         seed,
		Palette:      palettes[rng.Intn(len(palettes))],
		Boundary:     boundaryPoints(rng, 14),
		Placeables:   map[string]*Placeable{},
		exchangeRate: 1.0,
	}
	c.Placeables["agency-1"] = &Placeable{
		ID: "agency-1", Kind: KindNewsAgency, Subtype: "press", Icon: "newspaper",
		X: 0.5, Y: 0.42, HP: 100, MaxHP: 100, Value: CostAgency,
	}
	c.Placeables["mil-1"] = &Placeable{
		ID: "mil-1", Kind: KindMilitary, Subtype: "army", Icon: "shield",
		X: 0.58, Y: 0.6, HP: 100, MaxHP: 100, Value: CostMilitary,
		CooldownMax: 8,
	}
	return c
}

// boundaryPoints returns normalized boundary points; the client smooths them
// into a flowing closed curve (Catmull-Rom -> Bézier).
func boundaryPoints(rng *rand.Rand, n int) [][2]float64 {
	pts := make([][2]float64, n)
	for i := 0; i < n; i++ {
		ang := float64(i) / float64(n) * 2 * math.Pi
		rad := 0.30 + rng.Float64()*0.14
		pts[i] = [2]float64{
			0.5 + rad*math.Cos(ang),
			0.5 + rad*math.Sin(ang),
		}
	}
	return pts
}

// newPlaceable builds a cash-funded service (military or news agency).
func newPlaceable(id, kind, subtype string) *Placeable {
	switch PlaceableKind(kind) {
	case KindNewsAgency:
		return &Placeable{
			ID: id, Kind: KindNewsAgency, Subtype: "press", Icon: "newspaper",
			X: 0.5, Y: 0.5, HP: 100, MaxHP: 100, Value: CostAgency,
		}
	default: // military
		return &Placeable{
			ID: id, Kind: KindMilitary, Subtype: "army", Icon: "shield",
			X: 0.5, Y: 0.5, HP: 100, MaxHP: 100, Value: CostMilitary,
			CooldownMax: 8,
		}
	}
}
