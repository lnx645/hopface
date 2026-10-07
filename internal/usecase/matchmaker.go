// Package usecase berisi logika aplikasi: pencocokan, lobi chat, moderasi, akun.
// Hanya bergantung pada domain; tidak tahu soal HTTP, WebSocket, atau penyimpanan.
package usecase

import (
	"sync"
	"time"

	"hopface/internal/domain"
)

// Entry adalah satu orang yang sedang menunggu pasangan.
type Entry struct {
	ConnID  string
	UserID  string
	Profile domain.Profile
	Filter  domain.Filter
	Since   time.Time // kapan mulai menunggu; dipakai untuk melepas filter
}

// Pair adalah hasil pencocokan. A menunggu lebih lama dan bertindak sebagai penelepon.
type Pair struct{ A, B Entry }

// Matchmaker mencocokkan dua orang yang saling memenuhi filter satu sama lain.
// Setelah RelaxAfter, filter milik orang yang menunggu itu tidak lagi berlaku
// (filter lawan tetap berlaku sampai lawan juga melewati batas waktu).
type Matchmaker struct {
	mu         sync.Mutex
	waiting    []Entry
	recent     map[string]time.Time
	RelaxAfter time.Duration
	RecentTTL  time.Duration
	now        func() time.Time
}

// NewMatchmaker membuat matchmaker. clock boleh nil (memakai time.Now).
func NewMatchmaker(relaxAfter, recentTTL time.Duration, clock func() time.Time) *Matchmaker {
	if clock == nil {
		clock = time.Now
	}
	return &Matchmaker{recent: map[string]time.Time{}, RelaxAfter: relaxAfter, RecentTTL: recentTTL, now: clock}
}

func pairKey(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + "|" + b
}

func (m *Matchmaker) relaxed(e Entry, now time.Time) bool {
	return now.Sub(e.Since) >= m.RelaxAfter
}

// compatible: bukan orang yang sama, tidak baru saja dipasangkan, dan filter keduanya terpenuhi.
func (m *Matchmaker) compatible(a, b Entry, now time.Time) bool {
	if a.UserID == b.UserID {
		return false
	}
	if t, ok := m.recent[pairKey(a.UserID, b.UserID)]; ok && now.Sub(t) < m.RecentTTL {
		return false
	}
	okA := m.relaxed(a, now) || a.Filter.Matches(b.Profile)
	okB := m.relaxed(b, now) || b.Filter.Matches(a.Profile)
	return okA && okB
}

func (m *Matchmaker) remove(i int) Entry {
	e := m.waiting[i]
	m.waiting = append(m.waiting[:i], m.waiting[i+1:]...)
	return e
}

func (m *Matchmaker) paired(a, b Entry, now time.Time) Pair {
	m.recent[pairKey(a.UserID, b.UserID)] = now
	return Pair{A: a, B: b}
}

// Enqueue memasukkan e ke antrean. Bila langsung ada yang cocok, Pair dikembalikan.
// Entri lama dengan ConnID sama diganti.
func (m *Matchmaker) Enqueue(e Entry) (Pair, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.dequeueLocked(e.ConnID)
	if e.Since.IsZero() {
		e.Since = now
	}
	for i, w := range m.waiting {
		if m.compatible(w, e, now) {
			m.remove(i)
			return m.paired(w, e, now), true
		}
	}
	m.waiting = append(m.waiting, e)
	return Pair{}, false
}

func (m *Matchmaker) dequeueLocked(connID string) bool {
	for i, w := range m.waiting {
		if w.ConnID == connID {
			m.remove(i)
			return true
		}
	}
	return false
}

// Dequeue mengeluarkan koneksi dari antrean.
func (m *Matchmaker) Dequeue(connID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.dequeueLocked(connID)
}

// Tick mengevaluasi ulang seluruh antrean (misalnya setelah ada filter yang dilepas)
// dan mengembalikan semua pasangan baru. Juga membersihkan catatan "baru dipasangkan".
func (m *Matchmaker) Tick() []Pair {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	for k, t := range m.recent {
		if now.Sub(t) >= m.RecentTTL {
			delete(m.recent, k)
		}
	}
	var out []Pair
	for i := 0; i < len(m.waiting); i++ {
		for j := i + 1; j < len(m.waiting); j++ {
			if m.compatible(m.waiting[i], m.waiting[j], now) {
				b := m.remove(j)
				a := m.remove(i)
				out = append(out, m.paired(a, b, now))
				i-- // elemen i sudah bergeser, ulangi indeks yang sama
				break
			}
		}
	}
	return out
}

// Waiting jumlah orang yang sedang menunggu.
func (m *Matchmaker) Waiting() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.waiting)
}
