package usecase

import (
	"errors"
	"regexp"
	"sort"
	"time"

	"hopface/internal/domain"
)

// Social mengelola fitur Cari: orang terdekat, status, pertemanan, dan chat pertemanan.
type Social struct {
	users  domain.UserRepository
	social domain.SocialRepository
	now    func() time.Time
}

// NewSocial membuat layanan sosial.
func NewSocial(users domain.UserRepository, social domain.SocialRepository, clock func() time.Time) *Social {
	if clock == nil {
		clock = time.Now
	}
	return &Social{users: users, social: social, now: clock}
}

// Person adalah ringkasan pengguna untuk daftar Cari dan daftar teman.
type Person struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Age        int     `json:"age"`
	Gender     string  `json:"gender"`
	Avatar     string  `json:"avatar"`
	City       string  `json:"city"`
	DistanceKm float64 `json:"distanceKm"` // -1 bila lokasi tidak diketahui
	IsFriend   bool    `json:"isFriend"`
}

// PublicProfile adalah halaman profil orang lain.
type PublicProfile struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Age        int        `json:"age"`
	Gender     string     `json:"gender"`
	Avatar     string     `json:"avatar"`
	City       string     `json:"city"`
	DistanceKm float64    `json:"distanceKm"`
	IsFriend   bool       `json:"isFriend"`
	Posts      []PostView `json:"posts"`
}

// PostView adalah status yang disertai data penulisnya.
type PostView struct {
	ID     string    `json:"id"`
	UserID string    `json:"userId"`
	Name   string    `json:"name"`
	Avatar string    `json:"avatar"`
	Text   string    `json:"text"`
	Image  string    `json:"image,omitempty"`
	At     time.Time `json:"at"`
}

func personOf(u domain.User, me domain.User, now time.Time, isFriend bool) Person {
	p := Person{
		ID: u.ID, Name: u.Name, Age: u.Age(now), Gender: string(u.Gender),
		Avatar: u.DisplayAvatar(), IsFriend: isFriend, DistanceKm: -1,
	}
	if u.Geo != nil {
		p.City = u.Geo.City
		if me.Geo != nil {
			p.DistanceKm = domain.HaversineKm(me.Geo.Lat, me.Geo.Lon, u.Geo.Lat, u.Geo.Lon)
		}
	}
	return p
}

// Nearby mengembalikan pengguna terdekat milik pengguna saat ini.
// Hanya yang mengizinkan tampil (tidak HideNearby) dan profilnya lengkap.
func (s *Social) Nearby(uid string) ([]Person, error) {
	me, err := s.users.Get(uid)
	if err != nil {
		return nil, err
	}
	ids := s.visibleIDs()
	var out []Person
	for _, id := range ids {
		if id == uid {
			continue
		}
		u, err := s.users.Get(id)
		if err != nil || !u.ProfileComplete() {
			continue
		}
		out = append(out, personOf(u, me, s.now(), s.social.IsFriend(uid, id)))
	}
	// Terdekat dulu; lokasi tak diketahui (-1) di paling bawah.
	sort.SliceStable(out, func(i, j int) bool {
		di, dj := out[i].DistanceKm, out[j].DistanceKm
		if di < 0 {
			return false
		}
		if dj < 0 {
			return true
		}
		return di < dj
	})
	if len(out) > 100 {
		out = out[:100]
	}
	return out, nil
}

// visibleIDs mengambil semua ID pengguna yang mengizinkan tampil di Cari.
func (s *Social) visibleIDs() []string {
	all := s.users.All()
	ids := make([]string, 0, len(all))
	for _, u := range all {
		if u.HideNearby {
			continue
		}
		ids = append(ids, u.ID)
	}
	return ids
}

// PublicProfileOf mengambil halaman profil orang lain.
func (s *Social) PublicProfileOf(uid, target string) (PublicProfile, error) {
	me, err := s.users.Get(uid)
	if err != nil {
		return PublicProfile{}, err
	}
	u, err := s.users.Get(target)
	if err != nil {
		return PublicProfile{}, domain.ErrNotFound
	}
	if u.HideNearby {
		return PublicProfile{}, domain.ErrHidden
	}
	pp := PublicProfile{
		ID: u.ID, Name: u.Name, Age: u.Age(s.now()), Gender: string(u.Gender),
		Avatar: u.DisplayAvatar(), IsFriend: s.social.IsFriend(uid, target), DistanceKm: -1,
	}
	if u.Geo != nil {
		pp.City = u.Geo.City
		if me.Geo != nil {
			pp.DistanceKm = domain.HaversineKm(me.Geo.Lat, me.Geo.Lon, u.Geo.Lat, u.Geo.Lon)
		}
	} else {
		pp.DistanceKm = -1
	}
	for _, p := range s.social.PostsBy(target) {
		pp.Posts = append(pp.Posts, postView(p, u))
	}
	return pp, nil
}

func postView(p domain.Post, u domain.User) PostView {
	return PostView{ID: p.ID, UserID: p.UserID, Name: u.Name, Avatar: u.DisplayAvatar(), Text: p.Text, Image: p.Image, At: p.At}
}

// AddFriend menambahkan pertemanan dua arah.
func (s *Social) AddFriend(uid, target string) error {
	if uid == target {
		return errors.New("tidak bisa berteman dengan diri sendiri")
	}
	u, err := s.users.Get(target)
	if err != nil {
		return domain.ErrNotFound
	}
	if u.HideNearby {
		return domain.ErrHidden
	}
	return s.social.SaveFriend(domain.Friend{A: uid, B: target, At: s.now()})
}

// Friends mengembalikan daftar teman milik uid.
func (s *Social) Friends(uid string) ([]Person, error) {
	me, err := s.users.Get(uid)
	if err != nil {
		return nil, err
	}
	var out []Person
	for _, f := range s.social.FriendsOf(uid) {
		u, err := s.users.Get(f.Other(uid))
		if err != nil {
			continue
		}
		out = append(out, personOf(u, me, s.now(), true))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// CreatePost menyimpan status baru milik uid.
func (s *Social) CreatePost(uid, text, image string) (PostView, error) {
	u, err := s.users.Get(uid)
	if err != nil {
		return PostView{}, err
	}
	text = domain.NormalizeDMText(text)
	if len(text) == 0 && image == "" {
		return PostView{}, errors.New("status kosong")
	}
	if len([]rune(text)) > domain.PostTextMax {
		return PostView{}, domain.ErrTooLarge
	}
	p := domain.Post{ID: newID(), UserID: uid, Text: text, Image: image, At: s.now()}
	if err := s.social.SavePost(p); err != nil {
		return PostView{}, err
	}
	return postView(p, u), nil
}

// Feed mengembalikan status terbaru dari semua pengguna (terbaru di depan).
func (s *Social) Feed() []PostView {
	var out []PostView
	for _, p := range s.social.RecentPosts(100) {
		u, err := s.users.Get(p.UserID)
		if err != nil {
			continue
		}
		out = append(out, postView(p, u))
	}
	return out
}

var stickerRe = regexp.MustCompile(`^[a-z]{3,16}$`)
var uploadNameRe = regexp.MustCompile(`^[a-f0-9]{16}\.(png|jpg|gif|webp)$`)

// SendDM mengirim pesan dari uid ke teman target.
func (s *Social) SendDM(uid, target, kind, text string) (domain.Message, error) {
	if !s.social.IsFriend(uid, target) {
		return domain.Message{}, domain.ErrNotFriends
	}
	if !domain.ValidKind(kind) {
		return domain.Message{}, domain.ErrInvalidKind
	}
	switch kind {
	case domain.MsgText:
		text = domain.NormalizeDMText(text)
		if text == "" {
			return domain.Message{}, errors.New("pesan kosong")
		}
		if len([]rune(text)) > domain.DMTextMax {
			return domain.Message{}, domain.ErrTooLarge
		}
	case domain.MsgSticker:
		if !stickerRe.MatchString(text) {
			return domain.Message{}, domain.ErrInvalidKind
		}
	case domain.MsgImage:
		if !uploadNameRe.MatchString(text) {
			return domain.Message{}, domain.ErrInvalidKind
		}
	}
	m := domain.Message{ID: newID(), From: uid, To: target, Kind: kind, Text: text, At: s.now()}
	if err := s.social.SaveMessage(m); err != nil {
		return domain.Message{}, err
	}
	return m, nil
}

// DMs mengambil pesan dari teman target ke uid sejak waktu since (menurus, terbatas).
func (s *Social) DMs(uid, target string, since time.Time) ([]domain.Message, error) {
	if !s.social.IsFriend(uid, target) {
		return nil, domain.ErrNotFriends
	}
	return s.social.MessagesBetween(uid, target, since, 200), nil
}

// SetNearbyPref mengatur apakah pengguna tampil di listing Cari.
func (s *Social) SetNearbyPref(uid string, show bool) error {
	u, err := s.users.Get(uid)
	if err != nil {
		return err
	}
	u.HideNearby = !show
	return s.users.Save(u)
}
