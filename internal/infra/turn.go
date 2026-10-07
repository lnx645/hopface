package infra

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// publicSTUN dipakai bila kredensial TURN belum diatur (hanya cocok untuk pengembangan).
const publicSTUN = `{"iceServers":[{"urls":["stun:stun.cloudflare.com:3478"]}]}`

// CloudflareTURN meminta kredensial TURN berumur pendek dari Cloudflare.
// Token API tidak pernah dikirim ke browser; browser hanya menerima kredensial sementara.
// Hasil di-cache setengah dari TTL agar tidak memanggil Cloudflare di setiap permintaan.
type CloudflareTURN struct {
	KeyID, APIToken string
	TTL             time.Duration
	HTTP            *http.Client

	mu      sync.Mutex
	cached  json.RawMessage
	expires time.Time
	now     func() time.Time
}

// NewCloudflareTURN membuat klien. keyID/token kosong => hanya STUN publik.
func NewCloudflareTURN(keyID, token string, ttl time.Duration) *CloudflareTURN {
	if ttl <= 0 {
		ttl = time.Hour
	}
	return &CloudflareTURN{KeyID: keyID, APIToken: token, TTL: ttl, HTTP: &http.Client{Timeout: 10 * time.Second}, now: time.Now}
}

// Configured true bila TURN Cloudflare diatur.
func (c *CloudflareTURN) Configured() bool { return c.KeyID != "" && c.APIToken != "" }

// IceServers mengembalikan JSON {"iceServers":[...]} siap dipakai RTCPeerConnection.
func (c *CloudflareTURN) IceServers(ctx context.Context) (json.RawMessage, error) {
	if !c.Configured() {
		return json.RawMessage(publicSTUN), nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cached != nil && c.now().Before(c.expires) {
		return c.cached, nil
	}
	body, _ := json.Marshal(map[string]int{"ttl": int(c.TTL.Seconds())})
	u := fmt.Sprintf("https://rtc.live.cloudflare.com/v1/turn/keys/%s/credentials/generate-ice-servers", c.KeyID)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+c.APIToken)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("cloudflare status %d", res.StatusCode)
	}
	if !json.Valid(b) {
		return nil, fmt.Errorf("respons cloudflare bukan JSON")
	}
	c.cached, c.expires = b, c.now().Add(c.TTL/2)
	return b, nil
}
