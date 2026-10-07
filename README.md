# hopface

Chat video acak + sosial ala web 2013. Server Go (chi, clean architecture),
frontend Vue tanpa framework (Vite + SCSS), berjalan di port **8868**.

## Fitur

- **Chat video acak** — pencarian pasangan, filter usia/gender/negara,
  laporan (report) dengan bukti frame, moderasi admin.
- **Cari** — menu sosial baru:
  - deteksi **kota kasar dari IP** (bukan koordinat presisi — demi privasi);
    mode dev memakai lokasi tetap Jakarta;
  - listing orang terdekat (urut jarak), bisa sembunyi dari daftar;
  - halaman profil orang lain + tombol **tambah teman**;
  - posting **status** dengan gambar;
  - chat pertemanan: teks, **stiker emoji gaya 2013** (`/img/sticker/`),
    dan kirim gambar (polling AJAX 4 detik, gaya jadul).
- **Admin** — daftar laporan, bukti frame, blokir pengguna.

## Perintah

```sh
export HOME=/root          # wajib di mesin ini agar go/gh terbaca
make build-safe            # build frontend + gzip + build binary
sh deploy/install.sh       # deploy ke systemd hopface.service (:8868)
go test ./...              # seluruh test
```

Generator paket stiker (hanya saat stiker mau dibuat ulang):

```sh
go run ./cmd/genstickers   # menulis web/public/img/sticker/*.png
```

## Struktur

```
cmd/hopface/       wiring server
internal/domain/   aturan bisnis inti (tipe, error) — tanpa impor lapisan lain
internal/usecase/  layanan: accounts, lobby, moderation, social (Cari)
internal/infra/    penyimpanan JSON atomik, geo IP, avatar, signer
internal/delivery/ handler HTTP (chi) + WebSocket
web/               frontend Vue (src/) + aset statis (public/) → dist di-embed
deploy/            install.sh, unit systemd
```

Data runtime: `HOPFACE_DATA` (default `./data`, produksi `/var/lib/hopface`) —
`users.json`, `moderation.json`, `social.json`, `avatars/`, `uploads/`.
