// Perintah hopface: server chat video acak.
package main

import (
	"context"
	"crypto/rand"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"hopface/internal/delivery"
	"hopface/internal/infra"
	"hopface/internal/usecase"
	"hopface/web"
)

func env(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func main() {
	if err := run(); err != nil {
		log.Fatalf("hopface: %v", err)
	}
}

func run() error {
	// Port 8868 adalah default semua aplikasi web di mesin ini.
	addr := env("HOPFACE_ADDR", ":8868")
	baseURL := strings.TrimRight(env("HOPFACE_BASE_URL", "http://localhost:8868"), "/")
	dev := env("HOPFACE_DEV", "") == "1"
	dataDir := env("HOPFACE_DATA", "./data")

	secret := []byte(os.Getenv("HOPFACE_SESSION_SECRET"))
	if len(secret) == 0 {
		if !dev {
			return errors.New("HOPFACE_SESSION_SECRET wajib diisi (minimal 16 karakter) saat bukan mode dev")
		}
		secret = make([]byte, 32)
		_, _ = rand.Read(secret)
		log.Println("peringatan: HOPFACE_SESSION_SECRET kosong, memakai kunci acak (sesi hilang saat restart)")
	}
	signer, err := infra.NewSigner(secret)
	if err != nil {
		return err
	}

	users, err := infra.NewUserStore(dataDir)
	if err != nil {
		return err
	}
	modRepo, err := infra.NewModerationStore(dataDir)
	if err != nil {
		return err
	}
	avatarStore, err := infra.NewAvatarStore(dataDir)
	if err != nil {
		return err
	}

	ttl := 3600
	if v, err := strconv.Atoi(env("TURN_TTL", "")); err == nil && v > 0 {
		ttl = v
	}
	turn := infra.NewCloudflareTURN(os.Getenv("TURN_KEY_ID"), os.Getenv("TURN_API_TOKEN"), time.Duration(ttl)*time.Second)
	google := infra.NewGoogle(os.Getenv("GOOGLE_CLIENT_ID"), os.Getenv("GOOGLE_CLIENT_SECRET"), baseURL+"/auth/google/callback")

	admins := map[string]bool{}
	for _, e := range strings.Split(os.Getenv("HOPFACE_ADMIN_EMAILS"), ",") {
		if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
			admins[e] = true
		}
	}

	accounts := usecase.NewAccounts(users, avatarStore, nil)
	moderation := usecase.NewModeration(modRepo, nil)
	matchmaker := usecase.NewMatchmaker(10*time.Second, 30*time.Second, nil)
	lobby := usecase.NewLobby(matchmaker, moderation, nil)
	moderation.OnBan = lobby.Kick

	// Fitur Cari: lokasi kasar dari IP (mode dev memakai lokasi tetap Jakarta),
	// penyimpanan status/teman/pesan, dan unggahan gambar.
	if dev {
		accounts.SetGeoResolver(infra.DevGeo{})
	} else {
		accounts.SetGeoResolver(infra.NewGeoLookup())
	}
	socialStore, err := infra.NewSocialStore(dataDir)
	if err != nil {
		return err
	}
	uploadStore, err := infra.NewUploadStore(dataDir)
	if err != nil {
		return err
	}
	social := usecase.NewSocial(users, socialStore, nil)

	static, err := web.Dist()
	if err != nil {
		return err
	}
	handler := delivery.New(delivery.Deps{
		Cfg: delivery.Config{Addr: addr, BaseURL: baseURL, Dev: dev, AdminEmails: admins,
			SecureCookie: strings.HasPrefix(baseURL, "https://")},
		Accounts: accounts, Moderation: moderation, Lobby: lobby, Social: social,
		Signer: signer, Identity: google, ICE: turn, Static: static, Avatar: avatarStore, Uploads: uploadStore,
	})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Evaluasi ulang antrean tiap 500 ms agar filter yang melewati batas 10 detik segera dilepas.
	go func() {
		t := time.NewTicker(500 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				lobby.Tick()
			case <-ctx.Done():
				return
			}
		}
	}()

	srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Printf("hopface berjalan di %s (dev=%v, google=%v, turn=%v)", addr, dev, google.Configured(), turn.Configured())

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdown)
}
