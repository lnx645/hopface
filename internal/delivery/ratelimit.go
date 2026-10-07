package delivery

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// limiter membatasi jumlah kejadian per kunci dalam jendela waktu tetap.
type limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string]*bucket
	last   time.Time
}

type bucket struct {
	n     int
	reset time.Time
}

func newLimiter(max int, window time.Duration) *limiter {
	return &limiter{max: max, window: window, hits: map[string]*bucket{}}
}

func (l *limiter) allow(key string) bool { return l.allowMax(key, l.max) }

// allowMax seperti allow, tetapi dengan batas max khusus untuk pemanggilan ini.
func (l *limiter) allowMax(key string, max int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if now.Sub(l.last) > l.window { // bersihkan kunci kedaluwarsa agar memori tidak membengkak
		for k, b := range l.hits {
			if now.After(b.reset) {
				delete(l.hits, k)
			}
		}
		l.last = now
	}
	b := l.hits[key]
	if b == nil || now.After(b.reset) {
		b = &bucket{reset: now.Add(l.window)}
		l.hits[key] = b
	}
	b.n++
	return b.n <= max
}

// clientIP memakai X-Forwarded-For hanya bila koneksi datang dari proxy lokal (Caddy).
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			return strings.TrimSpace(strings.SplitN(xff, ",", 2)[0])
		}
	}
	return host
}

// sharedFactor: pengali batas untuk bucket bersama (lihat clientKey).
const sharedFactor = 25

// clientKey menentukan kunci limiter dan pengali batasnya.
//
// Bila alamat klien privat/loopback/tidak terbaca, IP asli pengunjung TIDAK terlihat. Di mesin ini
// server ada di belakang NAT, jadi setiap koneksi masuk tampak berasal dari gateway (10.10.0.1).
// Memakai alamat itu sebagai kunci membuat seluruh pengunjung berbagi satu batas kecil: satu orang
// (atau uji beban) memblokir semua orang. Karena itu alamat yang tidak bisa dibedakan dimasukkan ke
// satu bucket bersama dengan batas jauh lebih longgar. Perlindungan banjir tetap ada, tetapi tidak
// lagi memukul pengunjung sah. Batas per-pengguna (mis. /api/ice) tidak terpengaruh.
func clientKey(r *http.Request) (key string, factor int) {
	ip := clientIP(r)
	p := net.ParseIP(ip)
	if p == nil || p.IsPrivate() || p.IsLoopback() || p.IsLinkLocalUnicast() || p.IsUnspecified() {
		return "shared", sharedFactor
	}
	return ip, 1
}

func (s *Server) limit(l *limiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key, factor := clientKey(r)
			if !l.allowMax(key, l.max*factor) {
				w.Header().Set("Retry-After", "60")
				writeErr(w, http.StatusTooManyRequests, "rate_limited")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
