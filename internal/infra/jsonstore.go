// Package infra berisi implementasi teknis: penyimpanan, sesi, Google OAuth, TURN.
package infra

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"hopface/internal/domain"
)

// writeJSON menulis v ke path secara atomik (tulis berkas sementara lalu rename),
// sehingga berkas tidak rusak bila proses mati di tengah penulisan.
func writeJSON(path string, v interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readJSON(path string, v interface{}) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// UserStore menyimpan pengguna di satu berkas JSON, seluruhnya di memori.
// Cukup untuk puluhan ribu pengguna; ganti dengan database bila lebih besar.
type UserStore struct {
	mu    sync.RWMutex
	path  string
	users map[string]domain.User
}

// NewUserStore membuka (atau membuat) penyimpanan di dir.
func NewUserStore(dir string) (*UserStore, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &UserStore{path: filepath.Join(dir, "users.json"), users: map[string]domain.User{}}
	return s, readJSON(s.path, &s.users)
}

// Get implementasi domain.UserRepository.
func (s *UserStore) Get(id string) (domain.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[id]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	return u, nil
}

// Save implementasi domain.UserRepository.
func (s *UserStore) Save(u domain.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users[u.ID] = u
	return writeJSON(s.path, s.users)
}

// All implementasi domain.UserRepository (salinan seluruh pengguna).
func (s *UserStore) All() []domain.User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.User, 0, len(s.users))
	for _, u := range s.users {
		out = append(out, u)
	}
	return out
}

type modData struct {
	Bans    map[string]domain.Ban `json:"bans"`
	Reports []domain.Report       `json:"reports"`
}

const maxReports = 5000

// ModerationStore menyimpan blokir dan laporan.
type ModerationStore struct {
	mu   sync.Mutex
	path string
	d    modData
}

// NewModerationStore membuka (atau membuat) penyimpanan di dir.
func NewModerationStore(dir string) (*ModerationStore, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &ModerationStore{path: filepath.Join(dir, "moderation.json"), d: modData{Bans: map[string]domain.Ban{}}}
	if err := readJSON(s.path, &s.d); err != nil {
		return nil, err
	}
	if s.d.Bans == nil {
		s.d.Bans = map[string]domain.Ban{}
	}
	return s, nil
}

func (s *ModerationStore) flush() error { return writeJSON(s.path, s.d) }

// GetBan implementasi domain.ModerationRepository.
func (s *ModerationStore) GetBan(userID string) (domain.Ban, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.d.Bans[userID]
	return b, ok
}

// SaveBan implementasi domain.ModerationRepository.
func (s *ModerationStore) SaveBan(b domain.Ban) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.Bans[b.UserID] = b
	return s.flush()
}

// RemoveBan implementasi domain.ModerationRepository.
func (s *ModerationStore) RemoveBan(userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.d.Bans, userID)
	return s.flush()
}

// SaveReport implementasi domain.ModerationRepository.
func (s *ModerationStore) SaveReport(r domain.Report) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.Reports = append(s.d.Reports, r)
	if len(s.d.Reports) > maxReports {
		s.d.Reports = s.d.Reports[len(s.d.Reports)-maxReports:]
	}
	return s.flush()
}

// Reports mengembalikan salinan daftar laporan (terlama di depan).
func (s *ModerationStore) Reports() []domain.Report {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.Report(nil), s.d.Reports...)
}

// MarkHandled implementasi domain.ModerationRepository.
// Begitu laporan ditangani moderator, bukti frame dihapus agar penyimpanan tidak menumpuk.
func (s *ModerationStore) MarkHandled(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.d.Reports {
		if s.d.Reports[i].ID == id {
			s.d.Reports[i].Handled = true
			s.d.Reports[i].Frame = ""
			return s.flush()
		}
	}
	return domain.ErrNotFound
}
