package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"hopface/internal/domain"
)

type ctxKey struct{}

func userFrom(r *http.Request) domain.User {
	u, _ := r.Context().Value(ctxKey{}).(domain.User)
	return u
}

// requireUser memastikan ada sesi yang valid dan akun tidak diblokir.
func (s *Server) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieSession)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "login_required")
			return
		}
		uid, err := s.Signer.Verify(c.Value)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "login_required")
			return
		}
		u, err := s.Accounts.Get(uid)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "login_required")
			return
		}
		if _, banned := s.Moderation.IsBanned(u.ID); banned {
			writeErr(w, http.StatusForbidden, "banned")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
	})
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.isAdmin(userFrom(r)) {
			writeErr(w, http.StatusForbidden, "forbidden")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---- auth ----

func (s *Server) googleStart(w http.ResponseWriter, r *http.Request) {
	if !s.Identity.Configured() {
		http.Error(w, "Login Google belum dikonfigurasi", http.StatusServiceUnavailable)
		return
	}
	state := randHex(16)
	http.SetCookie(w, &http.Cookie{Name: cookieState, Value: state, Path: "/auth", MaxAge: 600,
		HttpOnly: true, Secure: s.Cfg.SecureCookie, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, s.Identity.AuthURL(state), http.StatusFound)
}

func (s *Server) googleCallback(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(cookieState)
	if err != nil || c.Value == "" || c.Value != r.URL.Query().Get("state") {
		http.Error(w, "State login tidak cocok, coba lagi.", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: cookieState, Value: "", Path: "/auth", MaxAge: -1})
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Redirect(w, r, "/?login=cancelled", http.StatusFound)
		return
	}
	id, err := s.Identity.Exchange(r.Context(), code)
	if err != nil || !id.EmailVerified {
		http.Redirect(w, r, "/?login=failed", http.StatusFound)
		return
	}
	u, err := s.Accounts.Upsert("g:"+id.Sub, strings.ToLower(id.Email), id.Name, id.Picture)
	if err != nil {
		http.Error(w, "Gagal menyimpan akun.", http.StatusInternalServerError)
		return
	}
	s.Accounts.RefreshGeo(u.ID, remoteIP(r)) // lokasi kasar untuk fitur Cari (best effort)
	s.setSession(w, u.ID)
	http.Redirect(w, r, "/", http.StatusFound)
}

// devLogin hanya aktif bila Cfg.Dev. Untuk pengembangan dan pengujian otomatis.
func (s *Server) devLogin(w http.ResponseWriter, r *http.Request) {
	if !s.Cfg.Dev {
		http.NotFound(w, r)
		return
	}
	email := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("email")))
	if email == "" {
		email = "dev@example.com"
	}
	name := r.URL.Query().Get("name")
	if name == "" {
		name = strings.SplitN(email, "@", 2)[0]
	}
	u, err := s.Accounts.Upsert("dev:"+email, email, name, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.Accounts.RefreshGeo(u.ID, remoteIP(r)) // mode dev: lokasi tetap dari DevGeo
	s.setSession(w, u.ID)
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) logout(w http.ResponseWriter, _ *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: cookieSession, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- API ----

type meResp struct {
	Authenticated   bool         `json:"authenticated"`
	GoogleEnabled   bool         `json:"googleEnabled"`
	Dev             bool         `json:"dev"`
	User            *domain.User `json:"user,omitempty"`
	Avatar          string       `json:"avatar"`
	Age             int          `json:"age,omitempty"`
	ProfileComplete bool         `json:"profileComplete"`
	Admin           bool         `json:"admin"`
	Banned          bool         `json:"banned"`
	SuggestCountry  string       `json:"suggestCountry,omitempty"`
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	resp := meResp{GoogleEnabled: s.Identity.Configured(), Dev: s.Cfg.Dev, SuggestCountry: suggestCountry(r)}
	if c, err := r.Cookie(cookieSession); err == nil {
		if uid, err := s.Signer.Verify(c.Value); err == nil {
			if u, err := s.Accounts.Get(uid); err == nil {
				resp.Authenticated = true
				resp.User = &u
				resp.Avatar = u.DisplayAvatar()
				resp.Age = u.Age(s.Now())
				resp.ProfileComplete = u.ProfileComplete()
				resp.Admin = s.isAdmin(u)
				_, resp.Banned = s.Moderation.IsBanned(u.ID)
			}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) saveProfile(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name      string        `json:"name"`
		Birthdate string        `json:"birthdate"`
		Gender    domain.Gender `json:"gender"`
		Country   string        `json:"country"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_profile")
		return
	}
	u, err := s.Accounts.UpdateProfile(userFrom(r).ID, in.Name, in.Birthdate, in.Gender, in.Country)
	switch {
	case errors.Is(err, domain.ErrUnderage):
		writeErr(w, http.StatusForbidden, "underage")
	case errors.Is(err, domain.ErrInvalidProfile):
		writeErr(w, http.StatusBadRequest, "invalid_profile")
	case err != nil:
		writeErr(w, http.StatusInternalServerError, "server_error")
	default:
		writeJSON(w, http.StatusOK, map[string]interface{}{"user": u, "age": u.Age(s.Now()), "profileComplete": u.ProfileComplete()})
	}
}

func (s *Server) ice(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if !u.ProfileComplete() {
		writeErr(w, http.StatusForbidden, "profile_incomplete")
		return
	}
	if !s.iceLimit.allow(u.ID) {
		writeErr(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	b, err := s.ICE.IceServers(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, "ice_unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(b)
}

// ---- admin ----

func (s *Server) adminReports(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{"reports": s.Moderation.Reports()})
}

func (s *Server) adminHandle(w http.ResponseWriter, r *http.Request) {
	if err := s.Moderation.Handle(chi.URLParam(r, "id")); err != nil {
		writeErr(w, http.StatusNotFound, "not_found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) adminBan(w http.ResponseWriter, r *http.Request) {
	var in struct {
		UserID string `json:"userId"`
		Reason string `json:"reason"`
		Hours  int    `json:"hours"` // 0 = permanen
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil || in.UserID == "" || in.Hours < 0 {
		writeErr(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if err := s.Moderation.Ban(in.UserID, in.Reason, time.Duration(in.Hours)*time.Hour); err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) adminUnban(w http.ResponseWriter, r *http.Request) {
	var in struct {
		UserID string `json:"userId"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil || in.UserID == "" {
		writeErr(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if err := s.Moderation.Unban(in.UserID); err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// uploadAvatar menerima satu berkas gambar (maks 2MB) dan menggantikan avatar.
func (s *Server) uploadAvatar(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(3 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_upload")
		return
	}
	f, _, err := r.FormFile("avatar")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "missing_avatar")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 2<<20))
	if err != nil || len(data) == 0 {
		writeErr(w, http.StatusBadRequest, "invalid_upload")
		return
	}
	u, err := s.Accounts.UpdateAvatar(userFrom(r).ID, data)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_image")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"avatar": u.DisplayAvatar()})
}

// resetAvatar menghapus avatar kustom; avatar kembali ke foto Google atau SVG bawaan.
func (s *Server) resetAvatar(w http.ResponseWriter, r *http.Request) {
	u, err := s.Accounts.RemoveAvatar(userFrom(r).ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"avatar": u.DisplayAvatar()})
}

// serveAvatar menyajikan berkas avatar bernama <hash>.png. Nama divalidasi sangat ketat
// (hex 16 + .png) sehingga tidak mungkin dipakai untuk membaca path sembarangan.
func (s *Server) serveAvatar(w http.ResponseWriter, r *http.Request) {
	if s.Avatar == nil {
		http.NotFound(w, r)
		return
	}
	path, ok := s.Avatar.AvatarPath(chi.URLParam(r, "name"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=60")
	http.ServeFile(w, r, path)
}

// defaultAvatar menyajikan SVG bawaan saat pengguna tak punya foto Google maupun unggahan.
func (s *Server) defaultAvatar(w http.ResponseWriter, r *http.Request) {
	f, err := s.Static.Open("avatar-default.svg")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=604800")
	st, _ := f.Stat()
	if rs, ok := f.(io.ReadSeeker); ok {
		http.ServeContent(w, r, "avatar-default.svg", st.ModTime(), rs)
		return
	}
	_, _ = io.Copy(w, f)
}
