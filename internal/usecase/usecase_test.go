package usecase

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"hopface/internal/domain"
)

// ---- alat bantu ----

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) Add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type fakeConn struct {
	id     string
	mu     sync.Mutex
	events []Event
	closed bool
}

func (f *fakeConn) ID() string { return f.id }
func (f *fakeConn) Send(e Event) {
	f.mu.Lock()
	f.events = append(f.events, e)
	f.mu.Unlock()
}
func (f *fakeConn) Close() { f.mu.Lock(); f.closed = true; f.mu.Unlock() }

func (f *fakeConn) last(typ string) *Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.events) - 1; i >= 0; i-- {
		if f.events[i].T == typ {
			e := f.events[i]
			return &e
		}
	}
	return nil
}

func (f *fakeConn) count(typ string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, e := range f.events {
		if e.T == typ {
			n++
		}
	}
	return n
}

func (f *fakeConn) state() string {
	if e := f.last("state"); e != nil {
		return e.State
	}
	return ""
}

func user(id string, g domain.Gender, age int, country string) domain.User {
	birth := time.Date(2026-age, 1, 1, 0, 0, 0, 0, time.UTC)
	return domain.User{ID: id, Name: id + " Lastname", Birthdate: birth.Format("2006-01-02"), Gender: g, Country: country}
}

func newLobby(clk *fakeClock) (*Lobby, *Matchmaker) {
	mm := NewMatchmaker(10*time.Second, 30*time.Second, clk.Now)
	return NewLobby(mm, nil, clk.Now), mm
}

func connect(l *Lobby, id string, u domain.User) *fakeConn {
	c := &fakeConn{id: "c-" + id}
	l.Connect(c, u)
	return c
}

var t0 = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

// ---- matchmaker ----

func TestMatchmakerMutualFilter(t *testing.T) {
	clk := &fakeClock{t: t0}
	mm := NewMatchmaker(10*time.Second, time.Minute, clk.Now)
	prof := func(id string, g domain.Gender, age int) domain.Profile {
		return domain.Profile{UserID: id, Gender: g, Age: age, Country: "ID"}
	}
	// A (pria, 30) mencari perempuan. B (perempuan, 25) mencari pria 18-28: A (30) tidak memenuhi filter B.
	a := Entry{ConnID: "a", UserID: "a", Profile: prof("a", domain.GenderMale, 30), Filter: domain.Filter{Gender: domain.GenderFemale}.Normalize()}
	b := Entry{ConnID: "b", UserID: "b", Profile: prof("b", domain.GenderFemale, 25), Filter: domain.Filter{Gender: domain.GenderMale, MaxAge: 28}.Normalize()}
	if _, ok := mm.Enqueue(a); ok {
		t.Fatal("tidak boleh cocok sendirian")
	}
	if _, ok := mm.Enqueue(b); ok {
		t.Fatal("filter B menolak A (30 > 28), tidak boleh cocok")
	}
	// C (pria, 27) memenuhi filter B, dan B memenuhi filter A (perempuan).
	c := Entry{ConnID: "c", UserID: "c", Profile: prof("c", domain.GenderMale, 27), Filter: domain.Filter{Gender: domain.GenderFemale}.Normalize()}
	p, ok := mm.Enqueue(c)
	if !ok {
		t.Fatal("C dan B saling cocok")
	}
	if p.A.UserID != "b" || p.B.UserID != "c" {
		t.Fatalf("pasangan salah: %s-%s", p.A.UserID, p.B.UserID)
	}
	if mm.Waiting() != 1 {
		t.Fatalf("A masih menunggu, waiting=%d", mm.Waiting())
	}
}

func TestMatchmakerRelaxAfter10Seconds(t *testing.T) {
	clk := &fakeClock{t: t0}
	mm := NewMatchmaker(10*time.Second, time.Minute, clk.Now)
	a := Entry{ConnID: "a", UserID: "a", Profile: domain.Profile{Gender: domain.GenderMale, Age: 30, Country: "ID"},
		Filter: domain.Filter{Country: "US"}.Normalize()} // A mau orang AS
	b := Entry{ConnID: "b", UserID: "b", Profile: domain.Profile{Gender: domain.GenderFemale, Age: 25, Country: "ID"}}
	b.Filter = domain.Filter{}.Normalize()
	mm.Enqueue(a)
	if _, ok := mm.Enqueue(b); ok {
		t.Fatal("filter negara A belum dilepas, tidak boleh cocok")
	}
	clk.Add(9 * time.Second)
	if len(mm.Tick()) != 0 {
		t.Fatal("belum 10 detik")
	}
	clk.Add(1 * time.Second)
	got := mm.Tick()
	if len(got) != 1 {
		t.Fatalf("setelah 10 detik filter A tidak berlaku, harus cocok; got %d", len(got))
	}
}

func TestMatchmakerRelaxedStillRespectsOtherFilter(t *testing.T) {
	clk := &fakeClock{t: t0}
	mm := NewMatchmaker(10*time.Second, time.Minute, clk.Now)
	a := Entry{ConnID: "a", UserID: "a", Profile: domain.Profile{Gender: domain.GenderMale, Age: 30, Country: "ID"},
		Filter: domain.Filter{Country: "US"}.Normalize()}
	// B hanya mau pria; A pria => B tidak menolak A. Tapi B menolak bila B mau perempuan.
	b := Entry{ConnID: "b", UserID: "b", Profile: domain.Profile{Gender: domain.GenderFemale, Age: 25, Country: "ID"},
		Filter: domain.Filter{Gender: domain.GenderFemale}.Normalize()}
	mm.Enqueue(a)
	mm.Enqueue(b)
	clk.Add(10 * time.Second)
	// A sudah melepas filter, B juga sudah (waktu tunggu sama) -> cocok.
	if len(mm.Tick()) != 1 {
		t.Fatal("keduanya sudah melewati 10 detik, harus cocok")
	}
}

func TestMatchmakerNoSelfNoRecent(t *testing.T) {
	clk := &fakeClock{t: t0}
	mm := NewMatchmaker(10*time.Second, 30*time.Second, clk.Now)
	e := func(conn, user string) Entry {
		return Entry{ConnID: conn, UserID: user, Profile: domain.Profile{Age: 20, Country: "ID"}, Filter: domain.Filter{}.Normalize()}
	}
	mm.Enqueue(e("c1", "u"))
	if _, ok := mm.Enqueue(e("c2", "u")); ok {
		t.Fatal("pengguna yang sama tidak boleh dipasangkan dengan dirinya")
	}
	mm2 := NewMatchmaker(10*time.Second, 30*time.Second, clk.Now)
	mm2.Enqueue(e("a", "a"))
	if _, ok := mm2.Enqueue(e("b", "b")); !ok {
		t.Fatal("harus cocok")
	}
	mm2.Enqueue(e("a", "a"))
	if _, ok := mm2.Enqueue(e("b", "b")); ok {
		t.Fatal("pasangan yang baru saja berpisah tidak boleh langsung dicocokkan lagi")
	}
	clk.Add(31 * time.Second)
	if len(mm2.Tick()) != 1 {
		t.Fatal("setelah masa jeda, boleh dicocokkan lagi")
	}
}

// ---- lobby ----

func TestLobbyMatchAndChat(t *testing.T) {
	clk := &fakeClock{t: t0}
	l, _ := newLobby(clk)
	a := connect(l, "a", user("a", domain.GenderMale, 30, "ID"))
	b := connect(l, "b", user("b", domain.GenderFemale, 25, "ID"))
	l.Start(a.id, domain.Filter{})
	if a.state() != StateSearching {
		t.Fatalf("a harus mencari, got %q", a.state())
	}
	l.Start(b.id, domain.Filter{})
	ma, mb := a.last("matched"), b.last("matched")
	if ma == nil || mb == nil {
		t.Fatal("keduanya harus menerima matched")
	}
	if ma.Role != "caller" || mb.Role != "callee" {
		t.Fatalf("peran salah: %s/%s", ma.Role, mb.Role)
	}
	if ma.Peer.Name != "b" || mb.Peer.Age != 30 {
		t.Fatalf("profil pasangan salah: %+v %+v", ma.Peer, mb.Peer)
	}
	l.Chat(a.id, "halo")
	if e := b.last("chat"); e == nil || e.Text != "halo" {
		t.Fatal("chat harus sampai ke b")
	}
	sig := json.RawMessage(`{"sdp":"x"}`)
	l.Signal(a.id, sig)
	if e := b.last("signal"); e == nil || string(e.Data) != string(sig) {
		t.Fatal("signal harus diteruskan apa adanya")
	}
}

// Aturan inti: B skip -> A otomatis mencari sampai ketemu, selama A belum Stop.
func TestLobbySkipKeepsOtherSearching(t *testing.T) {
	clk := &fakeClock{t: t0}
	l, mm := newLobby(clk)
	a := connect(l, "a", user("a", domain.GenderMale, 30, "ID"))
	b := connect(l, "b", user("b", domain.GenderFemale, 25, "ID"))
	l.Start(a.id, domain.Filter{})
	l.Start(b.id, domain.Filter{})
	if a.last("matched") == nil {
		t.Fatal("harus cocok dulu")
	}

	l.Next(b.id) // B skip
	if a.count("partner_left") != 1 {
		t.Fatal("A harus diberi tahu pasangan pergi")
	}
	if a.state() != StateSearching {
		t.Fatalf("A harus otomatis mencari lagi, state=%q", a.state())
	}
	if b.state() != StateSearching {
		t.Fatalf("B yang skip juga mencari, state=%q", b.state())
	}
	if mm.Waiting() != 2 {
		t.Fatalf("keduanya di antrean, waiting=%d", mm.Waiting())
	}

	// C datang; A (menunggu lebih dulu) dan B sama-sama di antrean, tetapi pasangan baru
	// A-B tidak boleh terulang segera, jadi C dicocokkan dengan salah satunya.
	c := connect(l, "c", user("c", domain.GenderFemale, 28, "ID"))
	l.Start(c.id, domain.Filter{})
	if c.last("matched") == nil {
		t.Fatal("C harus langsung dapat pasangan")
	}
	if a.count("matched") != 2 && b.count("matched") != 2 {
		t.Fatal("salah satu dari A/B harus ketemu C")
	}
}

func TestLobbyStopEndsAutoSearch(t *testing.T) {
	clk := &fakeClock{t: t0}
	l, mm := newLobby(clk)
	a := connect(l, "a", user("a", domain.GenderMale, 30, "ID"))
	b := connect(l, "b", user("b", domain.GenderFemale, 25, "ID"))
	l.Start(a.id, domain.Filter{})
	l.Start(b.id, domain.Filter{})

	l.Stop(a.id) // A berhenti
	if a.state() != StateIdle {
		t.Fatalf("A harus idle, got %q", a.state())
	}
	if b.state() != StateSearching {
		t.Fatalf("B harus mencari lagi karena belum Stop, got %q", b.state())
	}
	// B skip lagi tidak boleh menyeret A kembali mencari.
	l.Next(b.id)
	if a.state() != StateIdle {
		t.Fatal("A yang sudah Stop tidak boleh mencari lagi")
	}
	if mm.Waiting() != 1 {
		t.Fatalf("hanya B di antrean, waiting=%d", mm.Waiting())
	}
}

func TestLobbyDisconnectReSearchesPartner(t *testing.T) {
	clk := &fakeClock{t: t0}
	l, _ := newLobby(clk)
	a := connect(l, "a", user("a", domain.GenderMale, 30, "ID"))
	b := connect(l, "b", user("b", domain.GenderFemale, 25, "ID"))
	l.Start(a.id, domain.Filter{})
	l.Start(b.id, domain.Filter{})
	l.Disconnect(b.id)
	if a.state() != StateSearching {
		t.Fatalf("A harus kembali mencari saat B terputus, got %q", a.state())
	}
}

func TestLobbyFilterTimeoutViaTick(t *testing.T) {
	clk := &fakeClock{t: t0}
	l, _ := newLobby(clk)
	a := connect(l, "a", user("a", domain.GenderMale, 30, "ID"))
	b := connect(l, "b", user("b", domain.GenderFemale, 25, "ID"))
	l.Start(a.id, domain.Filter{Country: "US"}) // A cari orang AS
	l.Start(b.id, domain.Filter{})
	if a.last("matched") != nil {
		t.Fatal("belum boleh cocok")
	}
	clk.Add(10 * time.Second)
	l.Tick()
	if a.last("matched") == nil || b.last("matched") == nil {
		t.Fatal("setelah 10 detik filter dilepas dan keduanya harus cocok")
	}
}

func TestLobbyOneConnectionPerUser(t *testing.T) {
	clk := &fakeClock{t: t0}
	l, _ := newLobby(clk)
	u := user("a", domain.GenderMale, 30, "ID")
	old := connect(l, "a1", u)
	_ = connect(l, "a2", u)
	if !old.closed {
		t.Fatal("koneksi lama harus ditutup")
	}
	if e := old.last("error"); e == nil || e.Error != "replaced" {
		t.Fatal("koneksi lama harus diberi alasan 'replaced'")
	}
	if on, _ := l.Stats(); on != 1 {
		t.Fatalf("online harus 1, got %d", on)
	}
}

func TestLobbyChatLimits(t *testing.T) {
	clk := &fakeClock{t: t0}
	l, _ := newLobby(clk)
	a := connect(l, "a", user("a", domain.GenderMale, 30, "ID"))
	b := connect(l, "b", user("b", domain.GenderFemale, 25, "ID"))
	l.Start(a.id, domain.Filter{})
	l.Start(b.id, domain.Filter{})
	for i := 0; i < 8; i++ {
		l.Chat(a.id, "x")
	}
	if b.count("chat") != chatBurst {
		t.Fatalf("hanya %d pesan lolos dalam jendela, got %d", chatBurst, b.count("chat"))
	}
	if e := a.last("error"); e == nil || e.Error != "slow_down" {
		t.Fatal("pengirim harus diberi tahu slow_down")
	}
	clk.Add(chatWindow + time.Second)
	l.Chat(a.id, "lagi")
	if b.count("chat") != chatBurst+1 {
		t.Fatal("setelah jendela lewat pesan boleh dikirim lagi")
	}
	long := make([]rune, maxChatRunes+1)
	for i := range long {
		long[i] = 'a'
	}
	l.Chat(a.id, string(long))
	if e := a.last("error"); e == nil || e.Error != "message_too_long" {
		t.Fatal("pesan terlalu panjang harus ditolak")
	}
}

// ---- moderasi ----

type memMod struct {
	mu      sync.Mutex
	bans    map[string]domain.Ban
	reports []domain.Report
}

func (m *memMod) GetBan(id string) (domain.Ban, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.bans[id]
	return b, ok
}
func (m *memMod) SaveBan(b domain.Ban) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bans[b.UserID] = b
	return nil
}
func (m *memMod) RemoveBan(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.bans, id)
	return nil
}
func (m *memMod) SaveReport(r domain.Report) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reports = append(m.reports, r)
	return nil
}
func (m *memMod) Reports() []domain.Report {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]domain.Report(nil), m.reports...)
}
func (m *memMod) MarkHandled(string) error { return nil }

func TestReportMovesOnAndAutoBans(t *testing.T) {
	clk := &fakeClock{t: t0}
	repo := &memMod{bans: map[string]domain.Ban{}}
	mod := NewModeration(repo, clk.Now)
	kicked := make(chan string, 1)
	mod.OnBan = func(id string) { kicked <- id }
	// Jeda 30 detik seperti produksi: pelapor tidak boleh langsung dipasangkan lagi dengan yang dilaporkan.
	mm := NewMatchmaker(10*time.Second, 30*time.Second, clk.Now)
	l := NewLobby(mm, mod, clk.Now)

	bad := connect(l, "bad", user("bad", domain.GenderMale, 30, "ID"))
	l.Start(bad.id, domain.Filter{})
	for _, id := range []string{"r1", "r2", "r3"} {
		r := connect(l, id, user(id, domain.GenderFemale, 25, "ID"))
		l.Start(r.id, domain.Filter{})
		if r.last("matched") == nil {
			t.Fatalf("%s harus cocok dengan bad", id)
		}
		l.Chat(bad.id, "pesan buruk dari "+id)
		if err := l.Report(r.id, "kasar"); err != nil {
			t.Fatal(err)
		}
		if r.state() != StateSearching {
			t.Fatalf("setelah lapor %s harus pindah mencari, got %q", id, r.state())
		}
	}
	rs := repo.Reports()
	if len(rs) != 3 {
		t.Fatalf("harus 3 laporan, got %d", len(rs))
	}
	if len(rs[0].Transcript) == 0 || rs[0].Transcript[0] != "peer: pesan buruk dari r1" {
		t.Fatalf("transkrip harus berisi pesan terakhir, got %v", rs[0].Transcript)
	}
	if _, banned := mod.IsBanned("bad"); !banned {
		t.Fatal("3 pelapor berbeda harus memicu blokir otomatis")
	}
	select {
	case id := <-kicked:
		if id != "bad" {
			t.Fatalf("yang di-kick harus bad, got %s", id)
		}
	case <-time.After(time.Second):
		t.Fatal("OnBan harus dipanggil")
	}
	clk.Add(25 * time.Hour)
	if _, banned := mod.IsBanned("bad"); banned {
		t.Fatal("blokir 24 jam harus berakhir")
	}
}

func TestReportSameReporterCountsOnce(t *testing.T) {
	clk := &fakeClock{t: t0}
	repo := &memMod{bans: map[string]domain.Ban{}}
	mod := NewModeration(repo, clk.Now)
	for i := 0; i < 5; i++ {
		if _, err := mod.Report(domain.Report{ReporterID: "r1", ReportedID: "bad", Reason: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, banned := mod.IsBanned("bad"); banned {
		t.Fatal("satu pelapor yang sama tidak boleh memicu blokir otomatis")
	}
	if _, err := mod.Report(domain.Report{ReporterID: "x", ReportedID: "x"}); err == nil {
		t.Fatal("melaporkan diri sendiri harus ditolak")
	}
}
