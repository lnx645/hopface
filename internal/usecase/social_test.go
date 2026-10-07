package usecase

import (
	"errors"
	"strings"
	"testing"
	"time"

	"hopface/internal/domain"
)

// ---- alat bantu untuk fitur Cari ----

// memUsers adalah versi in-memory dari domain.UserRepository.
type memUsers struct{ m map[string]domain.User }

func newMemUsers() *memUsers { return &memUsers{m: map[string]domain.User{}} }

func (u *memUsers) Get(id string) (domain.User, error) {
	if x, ok := u.m[id]; ok {
		return x, nil
	}
	return domain.User{}, domain.ErrNotFound
}

func (u *memUsers) Save(x domain.User) error { u.m[x.ID] = x; return nil }

func (u *memUsers) All() []domain.User {
	out := make([]domain.User, 0, len(u.m))
	for _, x := range u.m {
		out = append(out, x)
	}
	return out
}

// memSocial adalah versi in-memory dari domain.SocialRepository.
type memSocial struct {
	posts   []domain.Post
	friends []domain.Friend
	msgs    []domain.Message
}

func (s *memSocial) SavePost(p domain.Post) error { s.posts = append(s.posts, p); return nil }
func (s *memSocial) PostsBy(uid string) []domain.Post {
	var out []domain.Post
	for i := len(s.posts) - 1; i >= 0; i-- {
		if s.posts[i].UserID == uid {
			out = append(out, s.posts[i])
		}
	}
	return out
}
func (s *memSocial) RecentPosts(limit int) []domain.Post {
	if limit > len(s.posts) {
		limit = len(s.posts)
	}
	out := make([]domain.Post, 0, limit)
	for i := len(s.posts) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, s.posts[i])
	}
	return out
}
func (s *memSocial) SaveFriend(f domain.Friend) error {
	if f.A > f.B {
		f.A, f.B = f.B, f.A
	}
	for _, x := range s.friends {
		if x.A == f.A && x.B == f.B {
			return nil
		}
	}
	s.friends = append(s.friends, f)
	return nil
}
func (s *memSocial) FriendsOf(uid string) []domain.Friend {
	var out []domain.Friend
	for _, f := range s.friends {
		if f.A == uid || f.B == uid {
			out = append(out, f)
		}
	}
	return out
}
func (s *memSocial) IsFriend(a, b string) bool {
	if a > b {
		a, b = b, a
	}
	for _, f := range s.friends {
		if f.A == a && f.B == b {
			return true
		}
	}
	return false
}
func (s *memSocial) SaveMessage(m domain.Message) error { s.msgs = append(s.msgs, m); return nil }
func (s *memSocial) MessagesBetween(a, b string, since time.Time, limit int) []domain.Message {
	if a > b {
		a, b = b, a
	}
	var out []domain.Message
	for _, m := range s.msgs {
		x, y := m.From, m.To
		if x > y {
			x, y = y, x
		}
		if x == a && y == b && m.At.After(since) {
			out = append(out, m)
		}
	}
	return out
}

func newSocialFixture() (*Social, *memUsers, *memSocial) {
	users := newMemUsers()
	social := &memSocial{}
	clk := &fakeClock{t: time.Date(2013, 6, 1, 12, 0, 0, 0, time.UTC)}
	users.Save(domain.User{ID: "me", Name: "Aku", Birthdate: "1990-01-01", Gender: domain.GenderMale,
		Country: "ID", CreatedAt: clk.Now(), Geo: &domain.Geo{City: "Jakarta", Lat: -6.2, Lon: 106.8, At: clk.Now()}})
	return NewSocial(users, social, clk.Now), users, social
}

func TestNearbySortsByDistance(t *testing.T) {
	s, users, _ := newSocialFixture()
	// Bandung ~120km, Surabaya ~660km, sembunyi, tanpa geo.
	users.Save(domain.User{ID: "bdg", Name: "Bandung", Birthdate: "1992-01-01", Gender: domain.GenderFemale,
		Country: "ID", CreatedAt: users.m["me"].CreatedAt, Geo: &domain.Geo{City: "Bandung", Lat: -6.9, Lon: 107.6}})
	users.Save(domain.User{ID: "sby", Name: "Surabaya", Birthdate: "1993-01-01", Gender: domain.GenderFemale,
		Country: "ID", CreatedAt: users.m["me"].CreatedAt, Geo: &domain.Geo{City: "Surabaya", Lat: -7.2, Lon: 112.7}})
	users.Save(domain.User{ID: "hidden", Name: "Sembunyi", Birthdate: "1994-01-01", Gender: domain.GenderOther,
		Country: "ID", CreatedAt: users.m["me"].CreatedAt, HideNearby: true,
		Geo: &domain.Geo{City: "Dekat", Lat: -6.21, Lon: 106.81}})
	users.Save(domain.User{ID: "nogeo", Name: "TanpaGeo", Birthdate: "1995-01-01", Gender: domain.GenderOther,
		Country: "ID", CreatedAt: users.m["me"].CreatedAt})

	people, err := s.Nearby("me")
	if err != nil {
		t.Fatalf("Nearby: %v", err)
	}
	if len(people) != 3 {
		t.Fatalf("jumlah = %d, mau 3 (hidden tidak boleh muncul)", len(people))
	}
	if people[0].ID != "bdg" {
		t.Errorf("urutan[0] = %s, mau bdg (terdekat)", people[0].ID)
	}
	if people[1].ID != "sby" {
		t.Errorf("urutan[1] = %s, mau sby", people[1].ID)
	}
	if people[2].ID != "nogeo" || people[2].DistanceKm != -1 {
		t.Errorf("lokasi tak dikenal harus di bawah dengan -1, got %s/%v", people[2].ID, people[2].DistanceKm)
	}
	if people[0].DistanceKm <= 0 || people[0].DistanceKm > 200 {
		t.Errorf("jarak bdg tak masuk akal: %v", people[0].DistanceKm)
	}
}

func TestHaversineJakartaBandung(t *testing.T) {
	d := domain.HaversineKm(-6.2, 106.8, -6.9, 107.6)
	if d < 80 || d > 160 {
		t.Errorf("Jakarta–Bandung = %.1f km, mau ~110-130", d)
	}
	if domain.HaversineKm(-6.2, 106.8, -6.2, 106.8) != 0 {
		t.Error("titik yang sama harus 0 km")
	}
}

func TestAddFriendAndDM(t *testing.T) {
	s, users, _ := newSocialFixture()
	users.Save(domain.User{ID: "b", Name: "Teman", Birthdate: "1991-01-01", Gender: domain.GenderFemale,
		Country: "ID", CreatedAt: users.m["me"].CreatedAt})

	// Belum berteman → ditolak.
	if _, err := s.SendDM("me", "b", domain.MsgText, "halo"); !errors.Is(err, domain.ErrNotFriends) {
		t.Fatalf("SendDM tanpa teman = %v, mau ErrNotFriends", err)
	}

	if err := s.AddFriend("me", "b"); err != nil {
		t.Fatalf("AddFriend: %v", err)
	}
	if !s.social.IsFriend("me", "b") {
		t.Fatal("pertemanan tidak tersimpan")
	}
	// Tambah diri sendiri ditolak.
	if err := s.AddFriend("me", "me"); err == nil {
		t.Error("berteman dengan diri sendiri harus ditolak")
	}

	// Teks biasa.
	m, err := s.SendDM("me", "b", domain.MsgText, "  halo juga  ")
	if err != nil || m.Text != "halo juga" {
		t.Fatalf("SendDM teks = %v/%v", m, err)
	}
	// Stiker valid & tidak valid.
	if _, err := s.SendDM("me", "b", domain.MsgSticker, "happy"); err != nil {
		t.Errorf("stiker happy ditolak: %v", err)
	}
	if _, err := s.SendDM("me", "b", domain.MsgSticker, "../../etc"); err == nil {
		t.Error("stiker berbahaya harus ditolak")
	}
	// Gambar wajib nama unggahan yang valid.
	if _, err := s.SendDM("me", "b", domain.MsgImage, "0123456789abcdef.png"); err != nil {
		t.Errorf("gambar valid ditolak: %v", err)
	}
	if _, err := s.SendDM("me", "b", domain.MsgImage, "/etc/passwd"); err == nil {
		t.Error("nama berkas berbahaya harus ditolak")
	}
	// Teks kepanjangan.
	if _, err := s.SendDM("me", "b", domain.MsgText, strings.Repeat("x", domain.DMTextMax+1)); !errors.Is(err, domain.ErrTooLarge) {
		t.Errorf("pesan kepanjangan = %v, mau ErrTooLarge", err)
	}

	got, err := s.DMs("me", "b", time.Time{})
	if err != nil || len(got) != 3 {
		t.Fatalf("DMs = %d pesan/%v, mau 3", len(got), err)
	}
	// Sejak sekarang → tidak ada pesan lama yang bocor.
	fresh := s.now().Add(time.Second)
	got, _ = s.DMs("b", "me", fresh)
	if len(got) != 0 {
		t.Errorf("DMs since = %d, mau 0", len(got))
	}
}

func TestPublicProfileHidden(t *testing.T) {
	s, users, _ := newSocialFixture()
	users.Save(domain.User{ID: "hid", Name: "Sembunyi", Birthdate: "1992-01-01", Gender: domain.GenderFemale,
		Country: "ID", CreatedAt: users.m["me"].CreatedAt, HideNearby: true})
	users.Save(domain.User{ID: "ok", Name: "Terlihat", Birthdate: "1992-01-01", Gender: domain.GenderFemale,
		Country: "ID", CreatedAt: users.m["me"].CreatedAt,
		Geo: &domain.Geo{City: "Depok", Lat: -6.4, Lon: 106.8}})

	if _, err := s.PublicProfileOf("me", "hid"); !errors.Is(err, domain.ErrHidden) {
		t.Errorf("profil tersembunyi = %v, mau ErrHidden", err)
	}
	if _, err := s.PublicProfileOf("me", "tidak-ada"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("profil hilang = %v, mau ErrNotFound", err)
	}
	pp, err := s.PublicProfileOf("me", "ok")
	if err != nil || pp.City != "Depok" || pp.DistanceKm < 0 {
		t.Errorf("PublicProfileOf = %+v/%v", pp, err)
	}
	// Sembunyi juga tidak bisa ditambah teman.
	if err := s.AddFriend("me", "hid"); !errors.Is(err, domain.ErrHidden) {
		t.Errorf("AddFriend ke yang sembunyi = %v, mau ErrHidden", err)
	}
}

func TestCreatePostAndFeed(t *testing.T) {
	s, users, social := newSocialFixture()
	users.Save(domain.User{ID: "b", Name: "Teman", Birthdate: "1991-01-01", Gender: domain.GenderFemale,
		Country: "ID", CreatedAt: users.m["me"].CreatedAt})

	if _, err := s.CreatePost("me", "   ", ""); err == nil {
		t.Error("status kosong harus ditolak")
	}
	if _, err := s.CreatePost("me", strings.Repeat("x", domain.PostTextMax+1), ""); !errors.Is(err, domain.ErrTooLarge) {
		t.Errorf("status kepanjangan = %v, mau ErrTooLarge", err)
	}
	if _, err := s.CreatePost("b", "halo dunia", ""); err != nil {
		t.Fatalf("CreatePost: %v", err)
	}
	if _, err := s.CreatePost("me", "baru saja", "0123456789abcdef.png"); err != nil {
		t.Fatalf("CreatePost dengan gambar: %v", err)
	}

	feed := s.Feed()
	if len(feed) != 2 {
		t.Fatalf("Feed = %d, mau 2", len(feed))
	}
	if feed[0].UserID != "me" || feed[0].Name != "Aku" || feed[0].Image == "" {
		t.Errorf("urutan feed salah: %+v", feed[0])
	}
	if feed[1].Text != "halo dunia" || social.posts[0].UserID != "b" {
		t.Errorf("feed[1] = %+v", feed[1])
	}
}

func TestSetNearbyPref(t *testing.T) {
	s, users, _ := newSocialFixture()
	users.Save(domain.User{ID: "b", Name: "Teman", Birthdate: "1991-01-01", Gender: domain.GenderFemale,
		Country: "ID", CreatedAt: users.m["me"].CreatedAt})
	if err := s.SetNearbyPref("me", false); err != nil {
		t.Fatalf("SetNearbyPref: %v", err)
	}
	if !users.m["me"].HideNearby {
		t.Error("show=false harus menghasilkan HideNearby=true")
	}
	people, err := s.Nearby("b")
	if err != nil || len(people) != 0 {
		t.Errorf("Nearby setelah sembunyi = %d orang/%v, mau 0", len(people), err)
	}
	// Nyalakan lagi → tampil.
	if err := s.SetNearbyPref("me", true); err != nil || users.m["me"].HideNearby {
		t.Errorf("show=true gagal: %v / HideNearby=%v", err, users.m["me"].HideNearby)
	}
}
