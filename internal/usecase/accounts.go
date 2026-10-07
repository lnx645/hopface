package usecase

import (
	"errors"
	"strings"
	"time"

	"hopface/internal/domain"
)

// AvatarSaver mengubah citra unggahan menjadi berkas PNG kecil di disk.
// Diimplementasikan di infra; AvatarSaver bernilai nil hanya pada pengujian.
type AvatarSaver interface {
	SaveAvatar(userID string, src []byte) (filename string, err error)
}

// GeoResolver mengubah IP menjadi lokasi kasar (kota) untuk fitur Cari.
// Diimplementasikan di infra; nil bila fitur lokasi tidak diaktifkan.
type GeoResolver interface {
	Resolve(ip string) (domain.Geo, error)
}

// Accounts mengelola akun pengguna dan profil.
type Accounts struct {
	users  domain.UserRepository
	avatar AvatarSaver
	geo    GeoResolver
	now    func() time.Time
}

// NewAccounts membuat layanan akun.
func NewAccounts(users domain.UserRepository, avatar AvatarSaver, clock func() time.Time) *Accounts {
	if clock == nil {
		clock = time.Now
	}
	return &Accounts{users: users, avatar: avatar, now: clock}
}

// Upsert menyimpan data identitas dari Google. Profil yang sudah ada (umur, gender, negara,
// nama kustom, avatar) tidak ditimpa. Nama dari Google hanya mengisi saat belum diubah pengguna.
func (a *Accounts) Upsert(id, email, name, picture string) (domain.User, error) {
	if id == "" {
		return domain.User{}, errors.New("id kosong")
	}
	u, err := a.users.Get(id)
	if err != nil {
		if !errors.Is(err, domain.ErrNotFound) {
			return u, err
		}
		u = domain.User{ID: id, CreatedAt: a.now()}
	}
	u.Email, u.Picture = email, picture
	if !u.NameCustom {
		u.Name = name
	}
	return u, a.users.Save(u)
}

// Get mengambil pengguna.
func (a *Accounts) Get(id string) (domain.User, error) { return a.users.Get(id) }

// SetGeoResolver memasang resolver lokasi kasar (dipanggil saat wiring).
func (a *Accounts) SetGeoResolver(r GeoResolver) { a.geo = r }

// RefreshGeo memperbarui lokasi kasar pengguna dari IP login. Best effort:
// kegagalan jaringan atau IP privat tidak membuat login gagal. Lokasi hanya
// disegarkan bila kosong atau lebih tua dari 7 hari.
func (a *Accounts) RefreshGeo(id, ip string) {
	if a.geo == nil || ip == "" {
		return
	}
	u, err := a.users.Get(id)
	if err != nil {
		return
	}
	if u.Geo != nil && a.now().Sub(u.Geo.At) < 7*24*time.Hour {
		return
	}
	g, err := a.geo.Resolve(ip)
	if err != nil || g.City == "" {
		return
	}
	g.At = a.now()
	u.Geo = &g
	_ = a.users.Save(u)
}

// UpdateProfile mengisi profil dengan validasi. Tanggal lahir terkunci setelah pertama kali diisi;
// nama yang disimpan selalu dianggap kustom sehingga login Google berikutnya tidak menimpanya.
func (a *Accounts) UpdateProfile(id, name, birthdate string, gender domain.Gender, country string) (domain.User, error) {
	u, err := a.users.Get(id)
	if err != nil {
		return u, err
	}
	name = strings.TrimSpace(name)
	if name != "" {
		if len([]rune(name)) > 60 {
			return u, domain.ErrInvalidProfile
		}
		u.Name = name
		u.NameCustom = true
	}
	if err := u.ApplyProfile(birthdate, gender, country, a.now()); err != nil {
		return u, err
	}
	return u, a.users.Save(u)
}

// UpdateAvatar memvalidasi dan menyimpan ulang avatar pengguna, lalu mengikatnya ke profil.
func (a *Accounts) UpdateAvatar(id string, src []byte) (domain.User, error) {
	u, err := a.users.Get(id)
	if err != nil {
		return u, err
	}
	if a.avatar == nil {
		return u, errors.New("avatar_unavailable")
	}
	filename, err := a.avatar.SaveAvatar(id, src)
	if err != nil {
		return u, err
	}
	u.AvatarCustom = "/avatar/" + filename
	return u, a.users.Save(u)
}

// RemoveAvatar menghapus avatar kustom sehingga avatar kembali ke foto Google atau bawaan.
func (a *Accounts) RemoveAvatar(id string) (domain.User, error) {
	u, err := a.users.Get(id)
	if err != nil {
		return u, err
	}
	u.AvatarCustom = ""
	return u, a.users.Save(u)
}
