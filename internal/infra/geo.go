package infra

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"hopface/internal/domain"
)

// ErrNoGeo dipulangkan bila IP tidak bisa dijadikan lokasi kasar
// (IP privat, IPv6, atau layanan geolokasi tidak bisa dihubungi).
var ErrNoGeo = errors.New("lokasi tidak dapat ditentukan")

// GeoLookup mengubah alamat IP publik menjadi kota kasar memakai ip-api.com.
// Hanya kota + titik perkiraan yang disimpan; tidak ada alamat atau koordinat presisi.
// Hasil di-cache per IP selama 7 hari agar tidak memanggil layanan terus-menerus.
type GeoLookup struct {
	client *http.Client
	mu     sync.Mutex
	cache  map[string]geoEntry
}

type geoEntry struct {
	g domain.Geo
	at time.Time
}

const geoCacheTTL = 7 * 24 * time.Hour

// NewGeoLookup membuat resolver geo dengan cache di memori.
func NewGeoLookup() *GeoLookup {
	return &GeoLookup{
		client: &http.Client{Timeout: 3 * time.Second},
		cache:  map[string]geoEntry{},
	}
}

// DevGeo adalah resolver lokasi tetap (Jakarta) untuk mode dev, agar fitur Cari
// bisa diuji tanpa panggil layanan luar dan tanpa IP publik.
type DevGeo struct{}

// Resolve mengembalikan lokasi tetap untuk pengembangan.
func (DevGeo) Resolve(string) (domain.Geo, error) {
	return domain.Geo{City: "Jakarta", Lat: -6.2, Lon: 106.8}, nil
}

// Resolve mengembalikan lokasi kasar untuk IP publik. IP privat/lokal langsung
// ditolak sehingga mode dev dan pengguna di LAN tidak memanggil layanan luar.
func (l *GeoLookup) Resolve(ip string) (domain.Geo, error) {
	p := net.ParseIP(ip)
	if p == nil || p.IsPrivate() || p.IsLoopback() || p.IsLinkLocalUnicast() || p.To4() == nil {
		return domain.Geo{}, ErrNoGeo
	}

	l.mu.Lock()
	if e, ok := l.cache[ip]; ok && time.Since(e.at) < geoCacheTTL {
		l.mu.Unlock()
		return e.g, nil
	}
	l.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("http://ip-api.com/json/%s?fields=status,message,city,lat,lon&lang=en", ip), nil)
	if err != nil {
		return domain.Geo{}, err
	}
	resp, err := l.client.Do(req)
	if err != nil {
		return domain.Geo{}, err
	}
	defer resp.Body.Close()
	var out struct {
		Status string  `json:"status"`
		City   string  `json:"city"`
		Lat    float64 `json:"lat"`
		Lon    float64 `json:"lon"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.Status != "success" || out.City == "" {
		return domain.Geo{}, ErrNoGeo
	}

	g := domain.Geo{City: out.City, Lat: out.Lat, Lon: out.Lon, At: time.Now().UTC()}
	l.mu.Lock()
	if len(l.cache) > 4096 { // jaga cache agar tidak membengkak
		l.cache = map[string]geoEntry{}
	}
	l.cache[ip] = geoEntry{g: g, at: time.Now()}
	l.mu.Unlock()
	return g, nil
}
