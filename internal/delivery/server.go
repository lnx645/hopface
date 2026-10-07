// Package delivery adalah lapisan HTTP/WebSocket (chi). Hanya menerjemahkan request
// ke pemanggilan usecase; tidak memuat aturan bisnis.
package delivery

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"hopface/internal/domain"
	"hopface/internal/infra"
	"hopface/internal/usecase"
)

// Config konfigurasi runtime.
type Config struct {
	Addr         string          // contoh ":8868"
	BaseURL      string          // contoh "https://hopface.example"
	Dev          bool            // aktifkan login palsu /auth/dev (JANGAN di produksi)
	AdminEmails  map[string]bool // email (huruf kecil) yang boleh membuka halaman admin
	SecureCookie bool            // cookie hanya lewat HTTPS
}

// Interface yang dibutuhkan delivery; diimplementasikan di infra.
type (
	// TokenSigner membuat dan memeriksa token sesi.
	TokenSigner interface {
		Issue(uid string, ttl time.Duration) string
		Verify(token string) (string, error)
	}
	// IdentityProvider penyedia login (Google).
	IdentityProvider interface {
		Configured() bool
		AuthURL(state string) string
		Exchange(ctx context.Context, code string) (infra.Identity, error)
	}
	// ICEProvider penyedia daftar server ICE (STUN/TURN).
	ICEProvider interface {
		IceServers(ctx context.Context) (json.RawMessage, error)
	}
)

// Deps seluruh ketergantungan server.
type Deps struct {
	Cfg        Config
	Accounts   *usecase.Accounts
	Moderation *usecase.Moderation
	Lobby      *usecase.Lobby
	Signer     TokenSigner
	Identity   IdentityProvider
	ICE        ICEProvider
	Static     fs.FS // hasil build frontend (di-embed)
	Avatar     *infra.AvatarStore
	Now        func() time.Time
}

// Server menyatukan handler.
type Server struct {
	Deps
	apiLimit  *limiter
	authLimit *limiter
	iceLimit  *limiter
}

const (
	cookieSession = "hf_session"
	cookieState   = "hf_oauth_state"
	sessionTTL    = 30 * 24 * time.Hour
)

// New membuat router siap pakai.
func New(d Deps) http.Handler {
	if d.Now == nil {
		d.Now = time.Now
	}
	s := &Server{Deps: d,
		apiLimit:  newLimiter(120, time.Minute),
		authLimit: newLimiter(20, time.Minute),
		iceLimit:  newLimiter(20, time.Minute),
	}
	r := chi.NewRouter()
	r.Use(securityHeaders)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })

	r.Route("/auth", func(r chi.Router) {
		r.Use(s.limit(s.authLimit))
		r.Get("/google", s.googleStart)
		r.Get("/google/callback", s.googleCallback)
		r.Get("/dev", s.devLogin)
		r.Post("/logout", s.logout)
	})

	r.Route("/api", func(r chi.Router) {
		r.Use(s.limit(s.apiLimit))
		r.Get("/me", s.me)
		r.Group(func(r chi.Router) {
			r.Use(s.requireUser)
			r.Post("/profile", s.saveProfile)
			r.Post("/profile/avatar", s.uploadAvatar)
			r.Post("/profile/avatar/reset", s.resetAvatar)
			r.Get("/ice", s.ice)
			r.Route("/admin", func(r chi.Router) {
				r.Use(s.requireAdmin)
				r.Get("/reports", s.adminReports)
				r.Post("/reports/{id}/handle", s.adminHandle)
				r.Post("/ban", s.adminBan)
				r.Post("/unban", s.adminUnban)
			})
		})
	})

	r.Get("/avatar/default.svg", s.defaultAvatar)
	r.Get("/avatar/{name}", s.serveAvatar)
	r.With(s.requireUser).Get("/ws", s.ws)
	r.NotFound(spaHandler(d.Static).ServeHTTP)
	return r
}

// ---- utilitas ----

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "camera=(self), microphone=(self), geolocation=()")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: https://*.googleusercontent.com; "+
			"style-src 'self' 'unsafe-inline'; connect-src 'self' ws: wss:; media-src 'self' blob:; "+
			"frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}

// suggestCountry menebak negara dari header Cloudflare atau Accept-Language (hanya saran awal).
func suggestCountry(r *http.Request) string {
	if c := domain.NormalizeCountry(r.Header.Get("CF-IPCountry")); c != "" {
		return c
	}
	for _, part := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		tag := strings.TrimSpace(strings.SplitN(part, ";", 2)[0])
		if i := strings.IndexByte(tag, '-'); i > 0 {
			if c := domain.NormalizeCountry(tag[i+1:]); c != "" {
				return c
			}
		}
	}
	return ""
}

func (s *Server) setSession(w http.ResponseWriter, uid string) {
	http.SetCookie(w, &http.Cookie{Name: cookieSession, Value: s.Signer.Issue(uid, sessionTTL), Path: "/",
		MaxAge: int(sessionTTL.Seconds()), HttpOnly: true, Secure: s.Cfg.SecureCookie, SameSite: http.SameSiteLaxMode})
}

func (s *Server) isAdmin(u domain.User) bool {
	return u.Email != "" && s.Cfg.AdminEmails[strings.ToLower(u.Email)]
}
