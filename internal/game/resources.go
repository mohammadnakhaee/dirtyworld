package game

// Resources are integer commodities players buy from the market and spend to
// build factories and nuclear weapons. They do not produce money themselves.

var resourceDefs = []struct {
	Name string
	Base float64 // base market price per unit
	Icon string
}{
	{"oil", 60, "🛢️"},
	{"grain", 30, "🌾"},
	{"water", 25, "💧"},
	{"coal", 45, "🪨"},
	{"wood", 35, "🪵"},
	{"iron", 70, "⛓️"},
	{"cotton", 50, "🧶"},
	{"gold", 130, "🪙"},
	{"glass", 85, "🔷"},
	{"plastic", 75, "🧴"},
	{"uranium", 260, "☢️"},
}

func basePrice(name string) float64 {
	for _, d := range resourceDefs {
		if d.Name == name {
			return d.Base
		}
	}
	return 50
}

func resourceIcon(name string) string {
	for _, d := range resourceDefs {
		if d.Name == name {
			return d.Icon
		}
	}
	return "•"
}

func resourceNames() []string {
	out := make([]string, len(resourceDefs))
	for i, d := range resourceDefs {
		out[i] = d.Name
	}
	return out
}
