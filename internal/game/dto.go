package game

import "math"

// Wire DTOs — the server controls exactly what each player sees, applying
// fog-of-war here rather than relying on struct tags.

type PlaceableDTO struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	Subtype     string   `json:"subtype"`
	Icon        string   `json:"icon"`
	X           float64  `json:"x"`
	Y           float64  `json:"y"`
	HP          float64  `json:"hp"`
	MaxHP       float64  `json:"maxHp"`
	Cooldown    int      `json:"cooldown"`
	CooldownMax int      `json:"cooldownMax"`
	Value       *float64 `json:"value,omitempty"`  // omitted in the un-spied attack view
	Payout      *float64 `json:"payout,omitempty"` // factories only, when known
}

type CountryDTO struct {
	PlayerID     string         `json:"playerId"`
	Name         string         `json:"name"`
	Currency     string         `json:"currency"`
	Palette      string         `json:"palette"`
	Boundary     [][2]float64   `json:"boundary"`
	Placeables   []PlaceableDTO `json:"placeables"`
	ExchangeRate float64        `json:"exchangeRate"`
}

type RivalDTO struct {
	CountryDTO
	Spied     bool           `json:"spied"`
	Resources map[string]int `json:"resources,omitempty"` // revealed only when spied
	Nukes     *int           `json:"nukes,omitempty"`
}

type CommodityDTO struct {
	Name    string    `json:"name"`
	Price   float64   `json:"price"`
	History []float64 `json:"history"`
}

type BoardEntry struct {
	PlayerID     string  `json:"playerId"`
	Name         string  `json:"name"`
	Currency     string  `json:"currency"`
	Palette      string  `json:"palette"`
	ExchangeRate float64 `json:"exchangeRate"`
	Self         bool    `json:"self"`
	Host         bool    `json:"host"`
	Connected    bool    `json:"connected"`
}

type SelfDTO struct {
	PlayerID   string         `json:"playerId"`
	Name       string         `json:"name"`
	Cash       float64        `json:"cash"`
	Confidence float64        `json:"confidence"`
	Capital    float64        `json:"capital"`
	Resources  map[string]int `json:"resources"`
	Nukes      int            `json:"nukes"`
	CanPost    bool           `json:"canPost"`
	IsHost     bool           `json:"isHost"`
}

// ---- static catalog (win rules, factory recipes, resource list) ----

type ResourceDTO struct {
	Name string  `json:"name"`
	Icon string  `json:"icon"`
	Base float64 `json:"base"`
}

type FactoryDTO struct {
	Key        string         `json:"key"`
	Title      string         `json:"title"`
	Icon       string         `json:"icon"`
	Recipe     map[string]int `json:"recipe"`
	BuildValue float64        `json:"buildValue"`
	Payout     float64        `json:"payout"`
}

type CatalogDTO struct {
	Resources     []ResourceDTO      `json:"resources"`
	Factories     []FactoryDTO       `json:"factories"`
	NukeRecipe    map[string]int     `json:"nukeRecipe"`
	NukeCash      float64            `json:"nukeCash"`
	NukeTarget    int                `json:"nukeTarget"`
	CapitalTarget float64            `json:"capitalTarget"`
	Services      map[string]float64 `json:"services"` // general-unit service prices
}

// gameCatalog is static, built once at package init.
var gameCatalog = buildCatalog()

func buildCatalog() CatalogDTO {
	c := CatalogDTO{
		NukeRecipe:    map[string]int{"oil": NukeOil, "uranium": NukeUranium},
		NukeCash:      NukeCash,
		NukeTarget:    NukeWinCount,
		CapitalTarget: CapitalWinTarget,
		Services: map[string]float64{
			"military": CostMilitary,
			"agency":   CostAgency,
			"spy":      SpyCost,
			"hack":     HackCost,
		},
	}
	for _, d := range resourceDefs {
		c.Resources = append(c.Resources, ResourceDTO{Name: d.Name, Icon: d.Icon, Base: d.Base})
	}
	for _, f := range factoryDefs {
		c.Factories = append(c.Factories, FactoryDTO{
			Key: f.Key, Title: f.Title, Icon: f.Icon, Recipe: f.Recipe,
			BuildValue: round(f.buildValue(), 1), Payout: round(f.payout(), 1),
		})
	}
	return c
}

// StateMsg is the full per-player view, sent on join (EvtSnapshot) and each
// tick (EvtTick). Fog-of-war is already applied per recipient.
type StateMsg struct {
	Tick        int            `json:"tick"`
	Started     bool           `json:"started"`
	Over        bool           `json:"over"`
	PlayerCount int            `json:"playerCount"`
	Self        SelfDTO        `json:"self"`
	Country     CountryDTO     `json:"country"`
	Rivals      []RivalDTO     `json:"rivals"`
	Market      []CommodityDTO `json:"market"`
	Board       []BoardEntry   `json:"board"`
	News        []*NewsItem    `json:"news"`
	Catalog     CatalogDTO     `json:"catalog"`
}

func round(v, step float64) float64 { return math.Round(v/step) * step }

func ownPlaceableDTO(pl *Placeable) PlaceableDTO {
	v := pl.Value
	d := PlaceableDTO{
		ID: pl.ID, Kind: string(pl.Kind), Subtype: pl.Subtype, Icon: pl.Icon,
		X: pl.X, Y: pl.Y, HP: round(pl.HP, 1), MaxHP: pl.MaxHP,
		Cooldown: pl.Cooldown, CooldownMax: pl.CooldownMax, Value: &v,
	}
	if pl.Kind == KindFactory {
		pay := pl.Payout
		d.Payout = &pay
	}
	return d
}

// rivalPlaceableDTO strips value/payout (and rounds HP) unless the viewer has
// spy access, in which case the true figures are revealed.
func rivalPlaceableDTO(pl *Placeable, spied bool) PlaceableDTO {
	d := PlaceableDTO{
		ID: pl.ID, Kind: string(pl.Kind), Subtype: pl.Subtype, Icon: pl.Icon,
		X: pl.X, Y: pl.Y, MaxHP: pl.MaxHP,
		Cooldown: pl.Cooldown, CooldownMax: pl.CooldownMax,
	}
	if spied {
		v := pl.Value
		d.Value = &v
		if pl.Kind == KindFactory {
			pay := pl.Payout
			d.Payout = &pay
		}
		d.HP = round(pl.HP, 1)
	} else {
		d.HP = round(pl.HP, 5) // rough HP only
	}
	return d
}
