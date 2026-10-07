// Package domain berisi aturan bisnis inti Hopface.
// Paket ini tidak boleh mengimpor apa pun dari lapisan lain (usecase, infra, delivery).
package domain

import (
	"errors"
	"strings"
	"time"
)

// Batas umur. Hopface hanya untuk dewasa.
const (
	MinAge = 18
	MaxAge = 99
)

// Gender yang dikenali sistem.
type Gender string

const (
	GenderAny    Gender = ""
	GenderMale   Gender = "male"
	GenderFemale Gender = "female"
	GenderOther  Gender = "other"
)

// Valid memeriksa apakah nilai gender dikenali (tidak termasuk "any").
func (g Gender) Valid() bool {
	return g == GenderMale || g == GenderFemale || g == GenderOther
}

// Kesalahan domain yang dipakai lintas lapisan.
var (
	ErrUnderage       = errors.New("kamu harus berumur minimal 18 tahun")
	ErrInvalidProfile = errors.New("data profil tidak valid")
	ErrBanned         = errors.New("akun ini sedang diblokir")
	ErrNotFound       = errors.New("data tidak ditemukan")
)

// User adalah pengguna terdaftar. ID berasal dari akun Google (sub).
type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	Name         string    `json:"name"` // nama tampilan; bila NameCustom, tidak ditimpa login berikutnya
	NameCustom   bool      `json:"nameCustom"`
	Picture      string    `json:"picture"`                // foto dari Google (mungkin kosong)
	AvatarCustom string    `json:"avatarCustom,omitempty"` // path /avatar/xxx.png setelah unggah sendiri
	Birthdate    string    `json:"birthdate"`              // format YYYY-MM-DD, terkunci setelah diisi
	Gender       Gender    `json:"gender"`
	Country      string    `json:"country"` // kode ISO 3166-1 alpha-2, huruf besar
	CreatedAt    time.Time `json:"createdAt"`
}

// DisplayAvatar memilih avatar yang ditampilkan dengan prioritas:
// unggahan sendiri > foto Google > avatar SVG bawaan.
func (u User) DisplayAvatar() string {
	if u.AvatarCustom != "" {
		return u.AvatarCustom
	}
	if u.Picture != "" {
		return u.Picture
	}
	return "/avatar/default.svg"
}

// ProfileComplete true bila semua data wajib sudah diisi.
func (u User) ProfileComplete() bool {
	return u.Birthdate != "" && u.Gender.Valid() && len(u.Country) == 2
}

// Age menghitung umur penuh pada waktu now. Mengembalikan 0 bila tanggal lahir kosong/rusak.
func (u User) Age(now time.Time) int {
	b, err := time.Parse("2006-01-02", u.Birthdate)
	if err != nil {
		return 0
	}
	return AgeAt(b, now)
}

// AgeAt menghitung umur penuh seseorang yang lahir pada birth.
func AgeAt(birth, now time.Time) int {
	age := now.Year() - birth.Year()
	if now.Month() < birth.Month() || (now.Month() == birth.Month() && now.Day() < birth.Day()) {
		age--
	}
	return age
}

// NormalizeCountry merapikan kode negara; string kosong bila tidak valid.
func NormalizeCountry(c string) string {
	c = strings.ToUpper(strings.TrimSpace(c))
	if len(c) != 2 {
		return ""
	}
	for _, r := range c {
		if r < 'A' || r > 'Z' {
			return ""
		}
	}
	return c
}

// ApplyProfile mengisi profil. Tanggal lahir terkunci: setelah terisi tidak bisa diubah
// agar umur tidak bisa dimanipulasi untuk menghindari batasan 18+.
func (u *User) ApplyProfile(birthdate string, gender Gender, country string, now time.Time) error {
	if !gender.Valid() {
		return ErrInvalidProfile
	}
	country = NormalizeCountry(country)
	if country == "" {
		return ErrInvalidProfile
	}
	if u.Birthdate == "" {
		b, err := time.Parse("2006-01-02", birthdate)
		if err != nil {
			return ErrInvalidProfile
		}
		age := AgeAt(b, now)
		if age < MinAge {
			return ErrUnderage
		}
		if age > MaxAge || b.After(now) {
			return ErrInvalidProfile
		}
		u.Birthdate = birthdate
	}
	u.Gender = gender
	u.Country = country
	return nil
}

// Profile adalah data publik yang dilihat pasangan chat. Tanpa email.
type Profile struct {
	UserID  string `json:"-"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
	Age     int    `json:"age"`
	Gender  Gender `json:"gender"`
	Country string `json:"country"`
}

// ProfileOf membentuk Profile publik dari User.
func ProfileOf(u User, now time.Time) Profile {
	return Profile{UserID: u.ID, Name: firstName(u.Name), Picture: u.DisplayAvatar(), Age: u.Age(now), Gender: u.Gender, Country: u.Country}
}

// firstName hanya menampilkan nama depan demi privasi.
func firstName(n string) string {
	n = strings.TrimSpace(n)
	if i := strings.IndexByte(n, ' '); i > 0 {
		n = n[:i]
	}
	if n == "" {
		return "Guest"
	}
	return n
}

// Filter adalah preferensi pencarian. Nilai kosong/0 berarti "bebas".
type Filter struct {
	Gender  Gender `json:"gender"`
	MinAge  int    `json:"minAge"`
	MaxAge  int    `json:"maxAge"`
	Country string `json:"country"`
}

// Normalize membatasi nilai filter ke rentang yang sah.
func (f Filter) Normalize() Filter {
	if f.Gender != GenderAny && !f.Gender.Valid() {
		f.Gender = GenderAny
	}
	if f.MinAge < MinAge {
		f.MinAge = MinAge
	}
	if f.MaxAge <= 0 || f.MaxAge > MaxAge {
		f.MaxAge = MaxAge
	}
	if f.MinAge > f.MaxAge {
		f.MinAge, f.MaxAge = f.MaxAge, f.MinAge
	}
	f.Country = NormalizeCountry(f.Country)
	return f
}

// Matches true bila profil p memenuhi filter.
func (f Filter) Matches(p Profile) bool {
	if f.Gender != GenderAny && f.Gender != p.Gender {
		return false
	}
	if p.Age < f.MinAge || p.Age > f.MaxAge {
		return false
	}
	if f.Country != "" && f.Country != p.Country {
		return false
	}
	return true
}
