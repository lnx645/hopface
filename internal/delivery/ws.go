package delivery

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"hopface/internal/domain"
	"hopface/internal/usecase"
)

const (
	wsReadLimit   = 512 << 10 // cukup untuk SDP dan bukti frame laporan (JPEG data URL); tetap membatasi pesan raksasa
	wsPongWait    = 60 * time.Second
	wsPingEvery   = 25 * time.Second
	wsWriteWait   = 10 * time.Second
	wsSendBuffer  = 64
	wsMsgBudget   = 300 // pesan masuk maksimal per jendela (ICE candidate bisa ramai)
	wsMsgWindowDu = 10 * time.Second
)

// Origin diperiksa secara bawaan oleh gorilla (harus sama dengan Host).
var upgrader = websocket.Upgrader{ReadBufferSize: 4096, WriteBufferSize: 4096}

// wsConn mengimplementasikan usecase.Conn. Send dan Close tidak pernah memblokir.
type wsConn struct {
	id   string
	ws   *websocket.Conn
	send chan []byte
	done chan struct{}
	once sync.Once
}

func (c *wsConn) ID() string { return c.id }

func (c *wsConn) Send(e usecase.Event) {
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	select {
	case c.send <- b:
	default: // klien terlalu lambat: putuskan daripada menahan lobi
		c.Close()
	}
}

func (c *wsConn) Close() { c.once.Do(func() { close(c.done) }) }

func (c *wsConn) writePump() {
	t := time.NewTicker(wsPingEvery)
	defer func() { t.Stop(); _ = c.ws.Close() }()
	for {
		select {
		case b := <-c.send:
			_ = c.ws.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if err := c.ws.WriteMessage(websocket.TextMessage, b); err != nil {
				return
			}
		case <-t.C:
			_ = c.ws.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if err := c.ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-c.done:
			// Kuras pesan yang sudah antre (mis. alasan pemutusan) sebelum menutup.
			for {
				select {
				case b := <-c.send:
					_ = c.ws.SetWriteDeadline(time.Now().Add(wsWriteWait))
					_ = c.ws.WriteMessage(websocket.TextMessage, b)
				default:
					_ = c.ws.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
					return
				}
			}
		}
	}
}

type clientMsg struct {
	T      string          `json:"t"`
	Filter domain.Filter   `json:"filter"`
	Text   string          `json:"text"`
	Data   json.RawMessage `json:"data"`
	Reason string          `json:"reason"`
	Frame  string          `json:"frame"` // data URL JPEG, frame video pasangan saat dilaporkan
}

func (s *Server) ws(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if !u.ProfileComplete() {
		writeErr(w, http.StatusForbidden, "profile_incomplete")
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade sudah menulis respons galat
	}
	c := &wsConn{id: randHex(8), ws: conn, send: make(chan []byte, wsSendBuffer), done: make(chan struct{})}
	conn.SetReadLimit(wsReadLimit)
	_ = conn.SetReadDeadline(time.Now().Add(wsPongWait))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(wsPongWait)) })

	go c.writePump()
	s.Lobby.Connect(c, u)
	defer func() { s.Lobby.Disconnect(c.id); c.Close() }()

	windowStart, count := time.Now(), 0
	for {
		var m clientMsg
		if err := conn.ReadJSON(&m); err != nil {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(wsPongWait))
		if now := time.Now(); now.Sub(windowStart) > wsMsgWindowDu {
			windowStart, count = now, 0
		}
		if count++; count > wsMsgBudget {
			c.Send(usecase.Event{T: "error", Error: "rate_limited"})
			return
		}
		switch m.T {
		case "start":
			s.Lobby.Start(c.id, m.Filter)
		case "next":
			s.Lobby.Next(c.id)
		case "stop":
			s.Lobby.Stop(c.id)
		case "chat":
			s.Lobby.Chat(c.id, m.Text)
		case "signal":
			s.Lobby.Signal(c.id, m.Data)
		case "report":
			if err := s.Lobby.Report(c.id, m.Reason, m.Frame); err != nil {
				c.Send(usecase.Event{T: "error", Error: "report_failed"})
			} else {
				log.Printf("laporan diterima: alasan=%q frame=%d byte", m.Reason, len(m.Frame))
				c.Send(usecase.Event{T: "reported"})
			}
		}
	}
}
