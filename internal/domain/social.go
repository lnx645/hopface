package domain

import (
	"errors"
	"math"
	"strings"
	"time"
)

// Geo adalah lokasi kasar dari alamat IP: hanya nama kota dan titik perkiraan
// untuk menghitung jarak. Tidak ada koordinat presisi (GPS) demi privasi.
type Geo struct {
	City string    `json:"city"`
	Lat  float64   `json:"lat"`
	Lon  float64   `json:"lon"`
	At   time.Time `json:"at"`
}

// Kelas pesan pertemanan.
const (
	MsgText    = "text"
	MsgImage   = "image"
	MsgSticker = "sticker"
)

// Batas konten sosial.
const (
	PostTextMax   = 500
	DMTextMax     = 500
	PostMaxKeep   = 500 // jumlah status yang disimpan per pengguna
	DMMaxPerPair  = 500 // jumlah pesan yang disimpan per pasangan
	ImageMaxBytes = 3 << 20
)

// Pesan salah kaprah/laporkan.
var (
	ErrNotFriends  = errors.New("kalian belum berteman")
	ErrTooLarge    = errors.New("konten terlalu besar")
	ErrInvalidKind = errors.New("jenis pesan tidak dikenal")
	ErrHidden      = errors.New("pengguna ini tidak tampil di Cari")
)

// Post adalah status yang dibagikan pengguna.
type Post struct {
	ID     string    `json:"id"`
	UserID string    `json:"userId"`
	Text   string    `json:"text"`
	Image  string    `json:"image,omitempty"` // nama berkas di /img/uploads/
	At     time.Time `json:"at"`
}

// Friend adalah pertemanan dua arah. A dan B disimpan terurut agar satu pasangan
// hanya punya satu baris.
type Friend struct {
	A  string    `json:"a"`
	B  string    `json:"b"`
	At time.Time `json:"at"`
}

// Other mengembalikan ID teman dari sudut pandang uid.
func (f Friend) Other(uid string) string {
	if f.A == uid {
		return f.B
	}
	return f.A
}

// Message adalah pesan chat pertemanan.
type Message struct {
	ID     string    `json:"id"`
	From   string    `json:"from"`
	To     string    `json:"to"`
	Kind   string    `json:"kind"` // text | image | sticker
	Text   string    `json:"text"` // isi teks / nama stiker / nama berkas gambar
	At     time.Time `json:"at"`
}

// ValidKind memeriksa jenis pesan yang didukung.
func ValidKind(k string) bool { return k == MsgText || k == MsgImage || k == MsgSticker }

// SocialRepository menyimpan status, pertemanan, dan pesan.
type SocialRepository interface {
	SavePost(p Post) error
	PostsBy(userID string) []Post
	RecentPosts(limit int) []Post

	SaveFriend(f Friend) error
	FriendsOf(userID string) []Friend
	IsFriend(a, b string) bool

	SaveMessage(m Message) error
	MessagesBetween(a, b string, since time.Time, limit int) []Message
}

// HaversineKm menghitung jarak antara dua koordinat dalam kilometer.
// Dipakai hanya dengan titik kota kasar, bukan lokasi presisi.
func HaversineKm(lat1, lon1, lat2, lon2 float64) float64 {
	const r = 6371.0
	const rad = math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLon := (lon2 - lon1) * rad
	sinLat := math.Sin(dLat / 2)
	sinLon := math.Sin(dLon / 2)
	h := sinLat*sinLat + math.Cos(lat1*rad)*math.Cos(lat2*rad)*sinLon*sinLon
	if h > 1 {
		h = 1
	}
	return 2 * r * math.Asin(math.Sqrt(h))
}

// NormalizeDMText memangkas teks pesan dan menghapus karakter kontrol.
func NormalizeDMText(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}
