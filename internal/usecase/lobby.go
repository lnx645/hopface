package usecase

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"hopface/internal/domain"
)

// Status koneksi.
const (
	StateIdle      = "idle"
	StateSearching = "searching"
	StateChatting  = "chatting"
)

// Event adalah pesan server -> klien (di-encode JSON oleh lapisan delivery).
type Event struct {
	T       string          `json:"t"` // state, matched, partner_left, chat, signal, error
	State   string          `json:"state,omitempty"`
	Role    string          `json:"role,omitempty"` // caller | callee
	Peer    *domain.Profile `json:"peer,omitempty"`
	Text    string          `json:"text,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
	Error   string          `json:"error,omitempty"`
	Online  int             `json:"online,omitempty"`
	Waiting int             `json:"waiting,omitempty"`
}

// Conn adalah satu koneksi klien. Send dan Close TIDAK BOLEH memblokir
// (Lobby memanggilnya saat memegang kunci).
type Conn interface {
	ID() string
	Send(Event)
	Close()
}

const (
	maxChatRunes   = 500
	transcriptSize = 20
	chatBurst      = 5
	chatWindow     = 5 * time.Second
)

type session struct {
	conn    Conn
	user    domain.User
	profile domain.Profile
	filter  domain.Filter
	state   string
	auto    bool // true selama pengguna belum menekan Stop: cari terus otomatis
	partner *session
	chat    []string
	sent    []time.Time
}

// Lobby menjalankan aturan pencarian: bila pasangan skip atau pergi dan pengguna
// belum menekan Stop, pengguna otomatis kembali mencari sampai ketemu.
type Lobby struct {
	mu     sync.Mutex
	mm     *Matchmaker
	mod    *Moderation
	now    func() time.Time
	byConn map[string]*session
	byUser map[string]*session
}

// NewLobby membuat lobi. mod boleh nil (laporan ditolak).
func NewLobby(mm *Matchmaker, mod *Moderation, clock func() time.Time) *Lobby {
	if clock == nil {
		clock = time.Now
	}
	return &Lobby{mm: mm, mod: mod, now: clock, byConn: map[string]*session{}, byUser: map[string]*session{}}
}

// Stats jumlah koneksi dan yang sedang menunggu.
func (l *Lobby) Stats() (online, waiting int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.byConn), l.mm.Waiting()
}

// Connect mendaftarkan koneksi. Satu pengguna hanya boleh punya satu koneksi aktif;
// koneksi lama diputus.
func (l *Lobby) Connect(c Conn, u domain.User) {
	l.mu.Lock()
	var old *session
	if o := l.byUser[u.ID]; o != nil {
		l.removeLocked(o)
		old = o
	}
	s := &session{conn: c, user: u, profile: domain.ProfileOf(u, l.now()), state: StateIdle}
	l.byConn[c.ID()] = s
	l.byUser[u.ID] = s
	l.sendState(s)
	l.mu.Unlock()
	if old != nil {
		old.conn.Send(Event{T: "error", Error: "replaced"})
		old.conn.Close()
	}
}

// Disconnect dipanggil saat koneksi terputus.
func (l *Lobby) Disconnect(connID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if s := l.byConn[connID]; s != nil {
		l.removeLocked(s)
	}
}

// Kick memutus koneksi aktif milik userID (misalnya setelah diblokir).
func (l *Lobby) Kick(userID string) {
	l.mu.Lock()
	s := l.byUser[userID]
	if s != nil {
		l.removeLocked(s)
	}
	l.mu.Unlock()
	if s != nil {
		s.conn.Send(Event{T: "error", Error: "banned"})
		s.conn.Close()
	}
}

func (l *Lobby) removeLocked(s *session) {
	l.mm.Dequeue(s.conn.ID())
	l.leave(s)
	delete(l.byConn, s.conn.ID())
	if l.byUser[s.user.ID] == s {
		delete(l.byUser, s.user.ID)
	}
	s.auto = false
}

func (l *Lobby) sendState(s *session) {
	s.conn.Send(Event{T: "state", State: s.state, Online: len(l.byConn), Waiting: l.mm.Waiting()})
}

// Start memulai pencarian dengan filter. Pencarian berlanjut otomatis sampai Stop.
func (l *Lobby) Start(connID string, f domain.Filter) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := l.byConn[connID]
	if s == nil {
		return
	}
	s.filter = f.Normalize()
	s.auto = true
	l.leave(s)
	l.search(s)
}

// Next melewati pasangan sekarang dan mencari yang baru.
func (l *Lobby) Next(connID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := l.byConn[connID]
	if s == nil || s.state == StateSearching {
		return
	}
	s.auto = true
	l.leave(s)
	l.search(s)
}

// Stop menghentikan pencarian otomatis dan memutus chat yang sedang berjalan.
func (l *Lobby) Stop(connID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := l.byConn[connID]
	if s == nil {
		return
	}
	s.auto = false
	l.mm.Dequeue(connID)
	l.leave(s)
	s.state = StateIdle
	l.sendState(s)
}

// Chat meneruskan pesan teks ke pasangan.
func (l *Lobby) Chat(connID, text string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := l.byConn[connID]
	if s == nil || s.partner == nil {
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if utf8.RuneCountInString(text) > maxChatRunes {
		s.conn.Send(Event{T: "error", Error: "message_too_long"})
		return
	}
	now := l.now()
	keep := s.sent[:0]
	for _, t := range s.sent {
		if now.Sub(t) < chatWindow {
			keep = append(keep, t)
		}
	}
	s.sent = keep
	if len(s.sent) >= chatBurst {
		s.conn.Send(Event{T: "error", Error: "slow_down"})
		return
	}
	s.sent = append(s.sent, now)
	addLine(s, "me: "+text)
	addLine(s.partner, "peer: "+text)
	s.partner.conn.Send(Event{T: "chat", Text: text})
}

func addLine(s *session, line string) {
	s.chat = append(s.chat, line)
	if len(s.chat) > transcriptSize {
		s.chat = s.chat[len(s.chat)-transcriptSize:]
	}
}

// Signal meneruskan data signaling WebRTC (offer/answer/ICE) ke pasangan.
func (l *Lobby) Signal(connID string, data json.RawMessage) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := l.byConn[connID]
	if s == nil || s.partner == nil {
		return
	}
	s.partner.conn.Send(Event{T: "signal", Data: data})
}

// Report melaporkan pasangan saat ini, lalu langsung pindah ke pasangan berikutnya.
func (l *Lobby) Report(connID, reason, frame string) error {
	if l.mod == nil {
		return errors.New("moderasi tidak aktif")
	}
	l.mu.Lock()
	s := l.byConn[connID]
	if s == nil || s.partner == nil {
		l.mu.Unlock()
		return errors.New("tidak sedang terhubung")
	}
	// Bukti foto: batasi ukuran supaya penyimpanan moderasi tidak meledak.
	if len(frame) > 400_000 {
		frame = ""
	}
	r := domain.Report{
		ReporterID: s.user.ID,
		ReportedID: s.partner.user.ID,
		Reason:     reason,
		Transcript: append([]string(nil), s.chat...),
		Frame:      frame,
	}
	l.mu.Unlock()

	// Penyimpanan ke disk dilakukan di luar kunci lobi agar tidak menahan pengguna lain.
	if _, err := l.mod.Report(r); err != nil {
		return err
	}
	l.Next(connID)
	return nil
}

// Tick dipanggil berkala: mengevaluasi ulang antrean (filter yang dilepas setelah timeout).
func (l *Lobby) Tick() {
	pairs := l.mm.Tick()
	if len(pairs) == 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, p := range pairs {
		l.match(p)
	}
}

func (l *Lobby) search(s *session) {
	s.state = StateSearching
	l.sendState(s)
	p, ok := l.mm.Enqueue(Entry{ConnID: s.conn.ID(), UserID: s.user.ID, Profile: s.profile, Filter: s.filter})
	if ok {
		l.match(p)
	}
}

func (l *Lobby) match(p Pair) {
	a, b := l.byConn[p.A.ConnID], l.byConn[p.B.ConnID]
	if a == nil || b == nil {
		// Salah satu sudah pergi; yang masih ada kembali ke antrean bila belum Stop.
		for _, s := range []*session{a, b} {
			if s != nil && s.auto {
				l.search(s)
			}
		}
		return
	}
	a.partner, b.partner = b, a
	a.chat, b.chat = nil, nil
	a.state, b.state = StateChatting, StateChatting
	pb, pa := b.profile, a.profile
	a.conn.Send(Event{T: "matched", Role: "caller", Peer: &pb})
	b.conn.Send(Event{T: "matched", Role: "callee", Peer: &pa})
}

// leave memutus hubungan s dengan pasangannya. Pasangan diberi tahu dan, bila
// belum menekan Stop, otomatis mencari lagi.
func (l *Lobby) leave(s *session) {
	s.chat = nil
	p := s.partner
	if p == nil {
		return
	}
	s.partner, p.partner = nil, nil
	p.chat = nil
	p.conn.Send(Event{T: "partner_left"})
	if p.auto {
		l.search(p)
	} else {
		p.state = StateIdle
		l.sendState(p)
	}
}
