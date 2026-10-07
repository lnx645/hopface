package infra

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	_ "image/gif"  // aktifkan format gif untuk image.Decode
	_ "image/jpeg" // aktifkan format jpeg
	"image/png"
	_ "image/png"
	stddraw "image/draw"
	"os"
	"path/filepath"
	"regexp"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // aktifkan format webp
)

// uk ukuran avatar hasil pemrosesan. 128px cukup untuk chip kecil dan detail.
const uk = 128

// AvatarStore menyimpan avatar sebagai PNG 128x128 di <dir>/avatars/<hash-uid>.png.
// Format PNG dipilih agar satu tipe berkas untuk semua masukan.
type AvatarStore struct {
	dir string
}

// NewAvatarStore membuat penyimpanan avatar di <dataDir>/avatars.
func NewAvatarStore(dataDir string) (*AvatarStore, error) {
	dir := filepath.Join(dataDir, "avatars")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &AvatarStore{dir: dir}, nil
}

var avatarNameRe = regexp.MustCompile(`^[a-f0-9]{16}\.png$`)

func hashUID(uid string) string {
	sum := sha256.Sum256([]byte("hf-avatar:" + uid))
	return hex.EncodeToString(sum[:8]) // 16 hex pertama
}

// SaveAvatar memvalidasi citra, menskalakannya ke 128x128, dan menulisnya atomik.
// Mengembalikan nama berkas (bukan path) untuk disajikan lewat /avatar/.
func (a *AvatarStore) SaveAvatar(userID string, src []byte) (string, error) {
	img, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return "", errors.New("berkas bukan gambar yang didukung (JPEG/PNG/GIF/WebP)")
	}
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, uk, uk))
	stddraw.Draw(dst, dst.Bounds(), image.NewUniform(color.RGBA{255, 255, 255, 255}), image.Point{}, stddraw.Src)
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)

	name := hashUID(userID) + ".png"
	tmp := filepath.Join(a.dir, name+".tmp")
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	if err := png.Encode(f, dst); err != nil {
		_ = f.Close()
		return "", err
	}
	_ = f.Close()
	if err := os.Rename(tmp, filepath.Join(a.dir, name)); err != nil {
		return "", err
	}
	return name, nil
}

// AvatarPath amankan nama berkas avatar untuk GetAvatar.
func (a *AvatarStore) AvatarPath(name string) (string, bool) {
	if !avatarNameRe.MatchString(name) {
		return "", false
	}
	return filepath.Join(a.dir, name), true
}
