// Command server wires the HTTP router, the room hub, the SSR pages and the
// WebSocket endpoint together. Run from the project root: `go run ./cmd/server`.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"geo-game/internal/game"
	"geo-game/internal/hub"
)

var tmpl *template.Template

func main() {
	var err error
	tmpl, err = template.ParseGlob("web/templates/*.html")
	if err != nil {
		log.Fatalf("parsing templates: %v", err)
	}

	h := hub.New()
	mux := http.NewServeMux()

	mux.Handle("GET /static/", http.StripPrefix("/static/",
		http.FileServer(http.Dir("web/static"))))

	mux.HandleFunc("GET /", landing)
	mux.HandleFunc("POST /create", makeCreate(h))
	mux.HandleFunc("POST /join", makeJoin(h))
	mux.HandleFunc("GET /play/{name}", makePlay(h, "game.html", "map"))
	mux.HandleFunc("GET /play/{name}/console", makePlay(h, "console.html", "console"))
	mux.HandleFunc("GET /ws/{name}", h.ServeWS)

	addr := ":8080"
	log.Printf("geo-game listening on http://localhost%s", addr)
	if err := http.ListenAndServe(addr, noCache(mux)); err != nil {
		log.Fatal(err)
	}
}

type landingData struct {
	Error string
	Room  string
}

func landing(w http.ResponseWriter, r *http.Request) {
	render(w, "landing.html", landingData{})
}

// identity holds the form fields shared by create and join. A player's only
// chosen identity is their country name; the currency is derived from it.
type identity struct {
	Room, Country string
}

func parseIdentity(r *http.Request) identity {
	_ = r.ParseForm()
	return identity{
		Room:    strings.TrimSpace(r.FormValue("room")),
		Country: strings.TrimSpace(r.FormValue("country")),
	}
}

func makeCreate(h *hub.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := parseIdentity(r)
		cfg := game.Config{
			StartingCash:  parseFloat(r.FormValue("treasury")),
			NukeWin:       int(parseFloat(r.FormValue("nukeWin"))),
			CapitalTarget: parseFloat(r.FormValue("capitalTarget")),
		}
		if _, err := h.CreateRoom(id.Room, cfg); err != nil {
			render(w, "landing.html", landingData{Error: err.Error(), Room: id.Room})
			return
		}
		http.Redirect(w, r, playURL(id), http.StatusSeeOther)
	}
}

// parseFloat returns 0 for blank/invalid input (Config.withDefaults handles 0).
func parseFloat(s string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return v
}

func makeJoin(h *hub.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := parseIdentity(r)
		if _, ok := h.GetRoom(id.Room); !ok {
			render(w, "landing.html", landingData{Error: "no room with that name", Room: id.Room})
			return
		}
		http.Redirect(w, r, playURL(id), http.StatusSeeOther)
	}
}

type playData struct {
	Room, Country, Currency, Token, Mode string
}

// makePlay serves either the map page or the console page. Both pages carry the
// same session Token so a player can open both as views of one country.
func makePlay(h *hub.Hub, templateName, mode string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if _, ok := h.GetRoom(name); !ok {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		country := def(r.URL.Query().Get("country"), "Unnamed")
		render(w, templateName, playData{
			Room:     name,
			Country:  country,
			Currency: country + " currency",
			Token:    def(r.URL.Query().Get("token"), genToken()),
			Mode:     mode,
		})
	}
}

func genToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func playURL(id identity) string {
	v := url.Values{}
	v.Set("country", def(id.Country, id.Room))
	return "/play/" + url.PathEscape(id.Room) + "?" + v.Encode()
}

func def(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

// noCache stops browsers from serving any stale response (pages, CSS, JS).
func noCache(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		h.ServeHTTP(w, r)
	})
}

func render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("render %s: %v", name, err)
	}
}
