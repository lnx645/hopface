package delivery

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"hopface/internal/domain"
)

// remoteIP mengambil IP asli klien: Cloudflare → X-Forwarded-For → RemoteAddr.
// Hanya dipakai untuk lokasi kasar (kota), bukan identitas.
func remoteIP(r *http.Request) string {
	if ip := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); ip != "" {
		return ip
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if ip := strings.TrimSpace(strings.SplitN(xff, ",", 2)[0]); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ---- Cari: orang terdekat ----

func (s *Server) nearbyList(w http.ResponseWriter, r *http.Request) {
	people, err := s.Social.Nearby(userFrom(r).ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"people": people})
}

func (s *Server) nearbyProfile(w http.ResponseWriter, r *http.Request) {
	p, err := s.Social.PublicProfileOf(userFrom(r).ID, chi.URLParam(r, "id"))
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not_found")
	case errors.Is(err, domain.ErrHidden):
		writeErr(w, http.StatusForbidden, "hidden")
	case err != nil:
		writeErr(w, http.StatusInternalServerError, "server_error")
	default:
		writeJSON(w, http.StatusOK, map[string]interface{}{"profile": p})
	}
}

func (s *Server) nearbyAddFriend(w http.ResponseWriter, r *http.Request) {
	err := s.Social.AddFriend(userFrom(r).ID, chi.URLParam(r, "id"))
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not_found")
	case errors.Is(err, domain.ErrHidden):
		writeErr(w, http.StatusForbidden, "hidden")
	case err != nil:
		writeErr(w, http.StatusBadRequest, "cannot_add")
	default:
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// nearbyPref menyalakan/mematikan tampilan pengguna di listing Cari.
func (s *Server) nearbyPref(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Show bool `json:"show"`
	}
	if err := jsonDecode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if err := s.Social.SetNearbyPref(userFrom(r).ID, in.Show); err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"show": in.Show})
}

func (s *Server) friendsList(w http.ResponseWriter, r *http.Request) {
	fs, err := s.Social.Friends(userFrom(r).ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"friends": fs})
}

// ---- status ----

func (s *Server) postsList(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{"posts": s.Social.Feed()})
}

// postsCreate menerima status baru: teks wajib di form dan gambar opsional (maks 3MB).
func (s *Server) postsCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(4 << 20); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		writeErr(w, http.StatusBadRequest, "invalid_upload")
		return
	}
	image := ""
	if f, _, err := r.FormFile("image"); err == nil {
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, domain.ImageMaxBytes+1))
		if err != nil || len(data) == 0 {
			writeErr(w, http.StatusBadRequest, "invalid_image")
			return
		}
		name, err := s.Uploads.Save(data)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_image")
			return
		}
		image = name
	}
	post, err := s.Social.CreatePost(userFrom(r).ID, r.FormValue("text"), image)
	switch {
	case errors.Is(err, domain.ErrTooLarge):
		writeErr(w, http.StatusRequestEntityTooLarge, "too_large")
	case err != nil:
		writeErr(w, http.StatusBadRequest, "invalid_post")
	default:
		writeJSON(w, http.StatusOK, map[string]interface{}{"post": post})
	}
}

// ---- chat pertemanan ----

func (s *Server) dmList(w http.ResponseWriter, r *http.Request) {
	since := time.Time{}
	if v := r.URL.Query().Get("since"); v != "" {
		if ms, err := strconv.ParseInt(v, 10, 64); err == nil && ms > 0 {
			since = time.Unix(0, ms*int64(time.Millisecond))
		}
	}
	msgs, err := s.Social.DMs(userFrom(r).ID, chi.URLParam(r, "id"), since)
	switch {
	case errors.Is(err, domain.ErrNotFriends):
		writeErr(w, http.StatusForbidden, "not_friends")
	case err != nil:
		writeErr(w, http.StatusInternalServerError, "server_error")
	default:
		writeJSON(w, http.StatusOK, map[string]interface{}{"messages": msgs})
	}
}

// dmSend mengirim pesan pertemanan: teks, stiker, atau gambar (multipart).
func (s *Server) dmSend(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(4 << 20); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		writeErr(w, http.StatusBadRequest, "invalid_upload")
		return
	}
	kind := r.FormValue("kind")
	text := r.FormValue("text")
	if f, _, err := r.FormFile("image"); err == nil {
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, domain.ImageMaxBytes+1))
		if err != nil || len(data) == 0 {
			writeErr(w, http.StatusBadRequest, "invalid_image")
			return
		}
		name, err := s.Uploads.Save(data)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_image")
			return
		}
		kind, text = domain.MsgImage, name
	}
	if kind == "" {
		kind = domain.MsgText
	}
	m, err := s.Social.SendDM(userFrom(r).ID, chi.URLParam(r, "id"), kind, text)
	switch {
	case errors.Is(err, domain.ErrNotFriends):
		writeErr(w, http.StatusForbidden, "not_friends")
	case errors.Is(err, domain.ErrTooLarge):
		writeErr(w, http.StatusRequestEntityTooLarge, "too_large")
	case errors.Is(err, domain.ErrInvalidKind):
		writeErr(w, http.StatusBadRequest, "invalid_message")
	case err != nil:
		writeErr(w, http.StatusBadRequest, "invalid_message")
	default:
		writeJSON(w, http.StatusOK, map[string]interface{}{"message": m})
	}
}

// serveUpload menyajikan gambar unggahan (status & pesan) dengan nama yang divalidasi ketat.
func (s *Server) serveUpload(w http.ResponseWriter, r *http.Request) {
	if s.Uploads == nil {
		http.NotFound(w, r)
		return
	}
	path, ok := s.Uploads.Path(chi.URLParam(r, "name"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	ext := path[strings.LastIndexByte(path, '.')+1:]
	ct := map[string]string{"png": "image/png", "jpg": "image/jpeg", "gif": "image/gif", "webp": "image/webp"}[ext]
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, path)
}

// jsonDecode membaca JSON tubuh request dengan batas ukuran aman.
func jsonDecode(r *http.Request, v interface{}) error {
	return json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(v)
}
