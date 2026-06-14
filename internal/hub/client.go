package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"geo-game/internal/game"
	"geo-game/internal/proto"
)

// Client is one browser connection. It implements game.Conn: the room pushes
// outbound frames through Send, and Close tears the socket down.
type Client struct {
	conn   *websocket.Conn
	room   *game.Room
	send   chan []byte
	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once
}

// Send queues a frame for the writer. Non-blocking: if the buffer is full the
// client is too slow, so we drop it and close rather than stall the room loop.
func (c *Client) Send(b []byte) {
	select {
	case c.send <- b:
	case <-c.ctx.Done():
	default:
		c.Close()
	}
}

// Close signals shutdown. The websocket itself is closed by writePump once it
// has flushed any frames still buffered (e.g. a rejection error sent right
// before the room drops the connection).
func (c *Client) Close() {
	c.once.Do(c.cancel)
}

// ServeWS upgrades the request and runs the connection's pumps. The room name
// comes from the path; identity (player/country/currency) from query params.
func (h *Hub) ServeWS(w http.ResponseWriter, req *http.Request) {
	name := req.PathValue("name")
	room, ok := h.GetRoom(name)
	if !ok {
		http.NotFound(w, req)
		return
	}

	conn, err := websocket.Accept(w, req, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // dev: accept any origin on localhost
	})
	if err != nil {
		return
	}
	conn.SetReadLimit(1 << 16)

	ctx, cancel := context.WithCancel(context.Background())
	cl := &Client{
		conn: conn, room: room, send: make(chan []byte, 64),
		ctx: ctx, cancel: cancel,
	}

	reply := make(chan error, 1)
	room.Register(&game.JoinReq{
		Conn:        cl,
		CountryName: sanitize(req.URL.Query().Get("country"), "Unnamed", 24),
		Token:       sanitize(req.URL.Query().Get("token"), "", 64),
		Reply:       reply,
	})
	// The room accepts or rejects synchronously. A rejection is delivered as the
	// WebSocket close reason with a distinct application code (4001) so the
	// client knows it's final and must NOT auto-reconnect.
	if err := <-reply; err != nil {
		conn.Close(StatusRejected, truncateReason(err.Error()))
		return
	}

	go cl.writePump()
	cl.readPump() // blocks until the socket closes
	room.Unregister(cl)
	cl.Close()
}

// StatusRejected is the WebSocket close code for a final join rejection
// (duplicate country name, game already started, room full).
const StatusRejected websocket.StatusCode = 4001

// truncateReason keeps the close reason within the protocol's 123-byte limit.
func truncateReason(s string) string {
	if len(s) > 120 {
		return s[:120]
	}
	return s
}

func (c *Client) readPump() {
	defer c.cancel()
	for {
		var env proto.Envelope
		typ, data, err := c.conn.Read(c.ctx)
		if err != nil {
			return
		}
		if typ != websocket.MessageText {
			continue
		}
		if err := json.Unmarshal(data, &env); err != nil {
			continue
		}
		c.room.Submit(game.Command{Conn: c, Type: env.Type, Raw: env.Payload})
	}
}

func (c *Client) writePump() {
	defer c.conn.Close(websocket.StatusNormalClosure, "")
	// write uses a fresh timeout per frame (not c.ctx) so a frame buffered just
	// before shutdown — e.g. a rejection error — still gets flushed.
	write := func(b []byte) error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return c.conn.Write(ctx, websocket.MessageText, b)
	}
	for {
		select {
		case b := <-c.send:
			if write(b) != nil {
				c.Close()
				return
			}
		case <-c.ctx.Done():
			// Drain and flush anything still queued, then stop.
			for {
				select {
				case b := <-c.send:
					if write(b) != nil {
						return
					}
				default:
					return
				}
			}
		}
	}
}

func sanitize(s, fallback string, max int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return fallback
	}
	if len(s) > max {
		s = s[:max]
	}
	return s
}
