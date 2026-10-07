package usecase

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"hopface/internal/domain"
)

// Moderation mengelola laporan dan blokir.
type Moderation struct {
	repo domain.ModerationRepository
	now  func() time.Time
	mu   sync.Mutex

	// Ambang blokir otomatis: AutoBanReports pelapor berbeda dalam AutoBanWindow.
	AutoBanReports int
	AutoBanWindow  time.Duration
	AutoBanFor     time.Duration

	// OnBan dipanggil setelah akun diblokir (untuk memutus koneksi yang sedang aktif).
	OnBan func(userID string)
}

// NewModeration membuat layanan moderasi dengan ambang bawaan: 3 pelapor / 24 jam => blokir 24 jam.
func NewModeration(repo domain.ModerationRepository, clock func() time.Time) *Moderation {
	if clock == nil {
		clock = time.Now
	}
	return &Moderation{repo: repo, now: clock, AutoBanReports: 3, AutoBanWindow: 24 * time.Hour, AutoBanFor: 24 * time.Hour}
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// IsBanned memeriksa blokir aktif.
func (m *Moderation) IsBanned(userID string) (domain.Ban, bool) {
	b, ok := m.repo.GetBan(userID)
	if !ok || !b.Active(m.now()) {
		return domain.Ban{}, false
	}
	return b, true
}

// Report menyimpan laporan dan memicu blokir otomatis bila ambang terlampaui.
func (m *Moderation) Report(r domain.Report) (domain.Report, error) {
	r.Reason = strings.TrimSpace(r.Reason)
	if r.ReporterID == "" || r.ReportedID == "" || r.ReporterID == r.ReportedID {
		return r, errors.New("laporan tidak valid")
	}
	if len(r.Reason) > 300 {
		r.Reason = r.Reason[:300]
	}
	r.ID, r.At = newID(), m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.repo.SaveReport(r); err != nil {
		return r, err
	}
	reporters := map[string]bool{}
	for _, x := range m.repo.Reports() {
		if x.ReportedID == r.ReportedID && m.now().Sub(x.At) <= m.AutoBanWindow {
			reporters[x.ReporterID] = true
		}
	}
	if len(reporters) >= m.AutoBanReports {
		if _, banned := m.IsBanned(r.ReportedID); !banned {
			_ = m.banLocked(r.ReportedID, "auto: banyak laporan", m.AutoBanFor)
		}
	}
	return r, nil
}

// Ban memblokir akun; d == 0 berarti permanen.
func (m *Moderation) Ban(userID, reason string, d time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.banLocked(userID, reason, d)
}

func (m *Moderation) banLocked(userID, reason string, d time.Duration) error {
	b := domain.Ban{UserID: userID, Reason: reason, At: m.now()}
	if d > 0 {
		b.Until = b.At.Add(d)
	}
	if err := m.repo.SaveBan(b); err != nil {
		return err
	}
	if m.OnBan != nil {
		go m.OnBan(userID)
	}
	return nil
}

// Unban mencabut blokir.
func (m *Moderation) Unban(userID string) error { return m.repo.RemoveBan(userID) }

// Reports daftar laporan, yang terbaru di depan.
func (m *Moderation) Reports() []domain.Report {
	rs := m.repo.Reports()
	for i, j := 0, len(rs)-1; i < j; i, j = i+1, j-1 {
		rs[i], rs[j] = rs[j], rs[i]
	}
	return rs
}

// Handle menandai laporan selesai ditinjau.
func (m *Moderation) Handle(id string) error { return m.repo.MarkHandled(id) }
