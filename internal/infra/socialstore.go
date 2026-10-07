package infra

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"

	_ "golang.org/x/image/webp"

	"hopface/internal/domain"
)

// ---- penyimpanan status, pertemanan, dan pesan ----

type socialData struct {
	Posts    []domain.Post    `json:"posts"`
	Friends  []domain.Friend  `json:"friends"`
	Messages []domain.Message `json:"messages"`
}

// SocialStore menyimpan data sosial di <dir>/social.json (seluruhnya di memori).
type SocialStore struct {
	mu sync.Mutex
	path string
	d    socialData
}

// NewSocialStore membuka (atau membuat) berkas sosial di dir.
func NewSocialStore(dir string) (*SocialStore, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &SocialStore{path: filepath.Join(dir, "social.json")}
	if err := readJSON(s.path, &s.d); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *SocialStore) flush() error { return writeJSON(s.path, s.d) }

// SavePost implementasi domain.SocialRepository. Menjaga maksimal domain.PostMaxKeep
// status per pengguna agar berkas tidak tumbuh tanpa batas.
func (s *SocialStore) SavePost(p domain.Post) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	per := map[string]int{}
	kept := s.d.Posts[:0]
	for i := len(s.d.Posts) - 1; i >= 0; i-- { // terbaru dulu
		if per[s.d.Posts[i].UserID] >= domain.PostMaxKeep {
			continue
		}
		per[s.d.Posts[i].UserID]++
		kept = append(kept, s.d.Posts[i])
	}
	// kembalikan urutan kronologis (append di atas membalik)
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	kept = append(kept, p)
	s.d.Posts = kept
	return s.flush()
}

// PostsBy implementasi domain.SocialRepository (terbaru di depan).
func (s *SocialStore) PostsBy(userID string) []domain.Post {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Post
	for i := len(s.d.Posts) - 1; i >= 0; i-- {
		if s.d.Posts[i].UserID == userID {
			out = append(out, s.d.Posts[i])
		}
	}
	return out
}

// RecentPosts implementasi domain.SocialRepository (terbaru di depan).
func (s *SocialStore) RecentPosts(limit int) []domain.Post {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 || limit > len(s.d.Posts) {
		limit = len(s.d.Posts)
	}
	out := make([]domain.Post, 0, limit)
	for i := len(s.d.Posts) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, s.d.Posts[i])
	}
	return out
}

// SaveFriend implementasi domain.SocialRepository (pasangan diurutkan, tanpa duplikat).
func (s *SocialStore) SaveFriend(f domain.Friend) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f.A > f.B {
		f.A, f.B = f.B, f.A
	}
	for _, x := range s.d.Friends {
		if x.A == f.A && x.B == f.B {
			return nil
		}
	}
	s.d.Friends = append(s.d.Friends, f)
	return s.flush()
}

// FriendsOf implementasi domain.SocialRepository.
func (s *SocialStore) FriendsOf(userID string) []domain.Friend {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Friend
	for _, f := range s.d.Friends {
		if f.A == userID || f.B == userID {
			out = append(out, f)
		}
	}
	return out
}

// IsFriend implementasi domain.SocialRepository.
func (s *SocialStore) IsFriend(a, b string) bool {
	if a > b {
		a, b = b, a
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range s.d.Friends {
		if f.A == a && f.B == b {
			return true
		}
	}
	return false
}

// SaveMessage implementasi domain.SocialRepository. Menjaga maksimal
// domain.DMMaxPerPair pesan per pasangan.
func (s *SocialStore) SaveMessage(m domain.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	pairKey := func(x domain.Message) string {
		a, b := x.From, x.To
		if a > b {
			a, b = b, a
		}
		return a + "|" + b
	}
	count := map[string]int{}
	kept := s.d.Messages[:0]
	for i := len(s.d.Messages) - 1; i >= 0; i-- {
		if count[pairKey(s.d.Messages[i])] >= domain.DMMaxPerPair {
			continue
		}
		count[pairKey(s.d.Messages[i])]++
		kept = append(kept, s.d.Messages[i])
	}
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	kept = append(kept, m)
	s.d.Messages = kept
	return s.flush()
}

// MessagesBetween implementasi domain.SocialRepository (menurus, dibatasi limit).
func (s *SocialStore) MessagesBetween(a, b string, since time.Time, limit int) []domain.Message {
	if a > b {
		a, b = b, a
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Message
	for _, m := range s.d.Messages {
		x, y := m.From, m.To
		if x > y {
			x, y = y, x
		}
		if x == a && y == b && m.At.After(since) {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

// ---- penyimpanan gambar unggahan (status & pesan) ----

// UploadStore menyimpan gambar unggahan di <dataDir>/uploads.
type UploadStore struct {
	dir string
}

// NewUploadStore membuat penyimpanan unggahan.
func NewUploadStore(dataDir string) (*UploadStore, error) {
	dir := filepath.Join(dataDir, "uploads")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &UploadStore{dir: dir}, nil
}

var uploadNameRe = regexp.MustCompile(`^[a-f0-9]{16}\.(png|jpg|gif|webp)$`)

// Save memvalidasi bahwa data benar-benar gambar (dengan mendecode-nya) lalu
// menyimpannya apa adanya. Mengembalikan nama berkas untuk disajikan lewat /img/uploads/.
func (u *UploadStore) Save(src []byte) (string, error) {
	if len(src) == 0 || len(src) > domain.ImageMaxBytes {
		return "", domain.ErrTooLarge
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(src))
	if err != nil || cfg.Width == 0 || cfg.Height == 0 {
		return "", errors.New("berkas bukan gambar yang didukung")
	}
	ext := map[string]string{"jpeg": "jpg", "png": "png", "gif": "gif", "webp": "webp"}[format]
	if ext == "" {
		return "", errors.New("format gambar tidak didukung")
	}
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	name := hex.EncodeToString(b) + "." + ext
	if err := os.WriteFile(filepath.Join(u.dir, name), src, 0o600); err != nil {
		return "", err
	}
	return name, nil
}

// Path mengembalikan path lengkap unggahan setelah nama divalidasi ketat.
func (u *UploadStore) Path(name string) (string, bool) {
	if !uploadNameRe.MatchString(name) {
		return "", false
	}
	return filepath.Join(u.dir, name), true
}
