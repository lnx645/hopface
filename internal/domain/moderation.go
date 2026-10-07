package domain

import "time"

// Ban adalah blokir akun. Until nol berarti permanen.
type Ban struct {
	UserID string    `json:"userId"`
	Reason string    `json:"reason"`
	At     time.Time `json:"at"`
	Until  time.Time `json:"until"`
}

// Active true bila blokir masih berlaku pada waktu now.
func (b Ban) Active(now time.Time) bool {
	return b.Until.IsZero() || now.Before(b.Until)
}

// Report adalah laporan pengguna terhadap pasangan chat-nya.
type Report struct {
	ID         string    `json:"id"`
	ReporterID string    `json:"reporterId"`
	ReportedID string    `json:"reportedId"`
	Reason     string    `json:"reason"`
	Transcript []string  `json:"transcript"`          // pesan teks terakhir, hanya disimpan bila dilaporkan
	Frame      string    `json:"frame,omitempty"`     // foto frame video pasangan saat dilaporkan (data URL JPEG)
	At         time.Time `json:"at"`
	Handled    bool      `json:"handled"`
}

// UserRepository menyimpan pengguna.
type UserRepository interface {
	Get(id string) (User, error)
	Save(u User) error
	All() []User // seluruh pengguna, dipakai listing Cari
}

// ModerationRepository menyimpan blokir dan laporan.
type ModerationRepository interface {
	GetBan(userID string) (Ban, bool)
	SaveBan(b Ban) error
	RemoveBan(userID string) error
	SaveReport(r Report) error
	Reports() []Report
	MarkHandled(id string) error
}
