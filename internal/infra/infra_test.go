package infra

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hopface/internal/domain"
)

func TestSigner(t *testing.T) {
	s, err := NewSigner([]byte("kunci-rahasia-panjang-sekali"))
	if err != nil {
		t.Fatal(err)
	}
	tok := s.Issue("g:123", time.Hour)
	if uid, err := s.Verify(tok); err != nil || uid != "g:123" {
		t.Fatalf("verify: %v %q", err, uid)
	}
	// diubah satu karakter pada payload -> ditolak
	i := strings.IndexByte(tok, '.')
	bad := tok[:i-1] + "A" + tok[i:]
	if bad == tok {
		bad = tok[:i-1] + "B" + tok[i:]
	}
	if _, err := s.Verify(bad); err == nil {
		t.Fatal("token yang diubah harus ditolak")
	}
	// kunci lain -> ditolak
	s2, _ := NewSigner([]byte("kunci-lain-yang-juga-panjang"))
	if _, err := s2.Verify(tok); err == nil {
		t.Fatal("kunci berbeda harus menolak")
	}
	// kedaluwarsa
	s.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	if _, err := s.Verify(tok); err == nil {
		t.Fatal("token kedaluwarsa harus ditolak")
	}
	if _, err := NewSigner([]byte("pendek")); err == nil {
		t.Fatal("kunci pendek harus ditolak")
	}
}

func TestStoresPersist(t *testing.T) {
	dir := t.TempDir()
	us, err := NewUserStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := us.Get("x"); err != domain.ErrNotFound {
		t.Fatalf("harus ErrNotFound, got %v", err)
	}
	if err := us.Save(domain.User{ID: "x", Name: "Xavier", Country: "ID"}); err != nil {
		t.Fatal(err)
	}
	us2, err := NewUserStore(dir) // buka ulang dari disk
	if err != nil {
		t.Fatal(err)
	}
	if u, err := us2.Get("x"); err != nil || u.Name != "Xavier" {
		t.Fatalf("data harus bertahan: %v %+v", err, u)
	}

	ms, _ := NewModerationStore(dir)
	_ = ms.SaveBan(domain.Ban{UserID: "x", Reason: "r"})
	_ = ms.SaveReport(domain.Report{ID: "1", ReporterID: "a", ReportedID: "x"})
	ms2, _ := NewModerationStore(dir)
	if _, ok := ms2.GetBan("x"); !ok || len(ms2.Reports()) != 1 {
		t.Fatal("ban dan laporan harus bertahan")
	}
	if err := ms2.MarkHandled("1"); err != nil || !ms2.Reports()[0].Handled {
		t.Fatal("MarkHandled gagal")
	}
	if err := ms2.RemoveBan("x"); err != nil {
		t.Fatal(err)
	}
	if _, ok := ms2.GetBan("x"); ok {
		t.Fatal("ban harus terhapus")
	}
}

func TestTURNCacheAndFallback(t *testing.T) {
	// tanpa kredensial: hanya STUN publik
	c := NewCloudflareTURN("", "", time.Hour)
	b, err := c.IceServers(context.Background())
	if err != nil || !strings.Contains(string(b), "stun:") || strings.Contains(string(b), "turn:") {
		t.Fatalf("fallback harus hanya STUN: %v %s", err, b)
	}

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer tok" || !strings.Contains(r.URL.Path, "/keys/kid/") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"iceServers":[{"urls":["turn:x"],"username":"u","credential":"c"}]}`))
	}))
	defer srv.Close()
	c = NewCloudflareTURN("kid", "tok", time.Hour)
	c.HTTP = srv.Client()
	c.HTTP.Transport = rewrite{srv.URL, http.DefaultTransport}
	for i := 0; i < 3; i++ {
		b, err := c.IceServers(context.Background())
		if err != nil || !json.Valid(b) || !strings.Contains(string(b), "credential") {
			t.Fatalf("ice: %v %s", err, b)
		}
	}
	if calls != 1 {
		t.Fatalf("hasil harus di-cache, Cloudflare dipanggil %d kali", calls)
	}
	c.expires = time.Now().Add(-time.Second) // paksa kedaluwarsa
	_, _ = c.IceServers(context.Background())
	if calls != 2 {
		t.Fatalf("setelah cache habis harus memanggil lagi, calls=%d", calls)
	}
}

// rewrite mengarahkan permintaan ke server uji.
type rewrite struct {
	base string
	rt   http.RoundTripper
}

func (r rewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	u := *req.URL
	u.Scheme, u.Host = "http", strings.TrimPrefix(r.base, "http://")
	req2 := req.Clone(req.Context())
	req2.URL = &u
	return r.rt.RoundTrip(req2)
}
