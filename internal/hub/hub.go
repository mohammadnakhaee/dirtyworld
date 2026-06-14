// Package hub holds the room registry and the WebSocket plumbing. It is the
// only place that imports both the websocket library and the game package; the
// game package never depends on it (it talks to connections via game.Conn).
package hub

import (
	"errors"
	"sync"

	"geo-game/internal/game"
)

var (
	ErrBadName   = errors.New("invalid room name")
	ErrNameTaken = errors.New("name not available")
)

// Hub is the process-wide registry of live rooms, keyed by name. The map is the
// only shared structure and is guarded by mu; everything else lives inside each
// room's goroutine.
type Hub struct {
	mu    sync.Mutex
	rooms map[string]*game.Room
}

func New() *Hub {
	return &Hub{rooms: map[string]*game.Room{}}
}

func validName(name string) bool {
	if name == "" || len(name) > 40 {
		return false
	}
	for _, c := range name {
		ok := c == '-' || c == '_' ||
			(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if !ok {
			return false
		}
	}
	return true
}

// CreateRoom registers a new room and launches its goroutine. Errors if the
// name is invalid or already in use.
func (h *Hub) CreateRoom(name string, cfg game.Config) (*game.Room, error) {
	if !validName(name) {
		return nil, ErrBadName
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, exists := h.rooms[name]; exists {
		return nil, ErrNameTaken
	}
	r := game.NewRoom(name, cfg)
	r.OnEmpty = h.dropRoom
	h.rooms[name] = r
	go r.Run()
	return r, nil
}

func (h *Hub) GetRoom(name string) (*game.Room, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	r, ok := h.rooms[name]
	return r, ok
}

// dropRoom is invoked from a room's goroutine when it empties out.
func (h *Hub) dropRoom(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.rooms, name)
}
