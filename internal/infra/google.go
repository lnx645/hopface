package infra

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Identity adalah data akun Google yang kita pakai.
type Identity struct {
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
}

// Google melakukan alur OAuth2 "authorization code" tanpa pustaka tambahan.
// Identitas diambil dari endpoint userinfo lewat TLS memakai access token yang
// baru kita tukar sendiri, sehingga tidak perlu memverifikasi tanda tangan ID token.
type Google struct {
	ClientID, ClientSecret, RedirectURL string
	HTTP                                *http.Client
}

// NewGoogle membuat klien Google.
func NewGoogle(id, secret, redirect string) *Google {
	return &Google{ClientID: id, ClientSecret: secret, RedirectURL: redirect, HTTP: &http.Client{Timeout: 10 * time.Second}}
}

// Configured true bila kredensial terisi.
func (g *Google) Configured() bool { return g.ClientID != "" && g.ClientSecret != "" }

// AuthURL membentuk URL persetujuan Google.
func (g *Google) AuthURL(state string) string {
	q := url.Values{
		"client_id":     {g.ClientID},
		"redirect_uri":  {g.RedirectURL},
		"response_type": {"code"},
		"scope":         {"openid email profile"},
		"state":         {state},
		"prompt":        {"select_account"},
	}
	return "https://accounts.google.com/o/oauth2/v2/auth?" + q.Encode()
}

// Exchange menukar code dengan identitas pengguna.
func (g *Google) Exchange(ctx context.Context, code string) (Identity, error) {
	form := url.Values{
		"code":          {code},
		"client_id":     {g.ClientID},
		"client_secret": {g.ClientSecret},
		"redirect_uri":  {g.RedirectURL},
		"grant_type":    {"authorization_code"},
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://oauth2.googleapis.com/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := g.do(req, &tok); err != nil {
		return Identity{}, fmt.Errorf("tukar kode: %w", err)
	}
	if tok.AccessToken == "" {
		return Identity{}, errors.New("access token kosong")
	}
	req, _ = http.NewRequestWithContext(ctx, http.MethodGet, "https://openidconnect.googleapis.com/v1/userinfo", nil)
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	var id Identity
	if err := g.do(req, &id); err != nil {
		return Identity{}, fmt.Errorf("userinfo: %w", err)
	}
	if id.Sub == "" {
		return Identity{}, errors.New("akun Google tanpa id")
	}
	return id, nil
}

func (g *Google) do(req *http.Request, out interface{}) error {
	res, err := g.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", res.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(out)
}
