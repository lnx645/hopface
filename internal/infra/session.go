package infra

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Signer menandatangani token sesi dengan HMAC-SHA256 (tanpa dependensi luar).
type Signer struct {
	key []byte
	now func() time.Time
}

// NewSigner membuat penanda tangan. key minimal 16 byte.
func NewSigner(key []byte) (*Signer, error) {
	if len(key) < 16 {
		return nil, errors.New("kunci sesi terlalu pendek (minimal 16 byte)")
	}
	return &Signer{key: key, now: time.Now}, nil
}

type claims struct {
	UID string `json:"u"`
	Exp int64  `json:"e"`
}

func (s *Signer) mac(payload string) string {
	h := hmac.New(sha256.New, s.key)
	h.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

// Issue membuat token untuk uid yang berlaku selama ttl.
func (s *Signer) Issue(uid string, ttl time.Duration) string {
	b, _ := json.Marshal(claims{UID: uid, Exp: s.now().Add(ttl).Unix()})
	p := base64.RawURLEncoding.EncodeToString(b)
	return p + "." + s.mac(p)
}

// Verify memeriksa tanda tangan dan masa berlaku; mengembalikan uid.
func (s *Signer) Verify(token string) (string, error) {
	i := strings.IndexByte(token, '.')
	if i < 0 {
		return "", errors.New("token rusak")
	}
	p, sig := token[:i], token[i+1:]
	if !hmac.Equal([]byte(sig), []byte(s.mac(p))) {
		return "", errors.New("tanda tangan tidak cocok")
	}
	raw, err := base64.RawURLEncoding.DecodeString(p)
	if err != nil {
		return "", errors.New("token rusak")
	}
	var c claims
	if err := json.Unmarshal(raw, &c); err != nil || c.UID == "" {
		return "", errors.New("token rusak")
	}
	if s.now().Unix() > c.Exp {
		return "", errors.New("token kedaluwarsa")
	}
	return c.UID, nil
}
