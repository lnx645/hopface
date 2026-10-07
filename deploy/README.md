# Deployment Hopface

Port default **8868** (`http://localhost:8868`). Satu binary Go, frontend Vue sudah di-embed.

## Build

```sh
make build-safe     # build dengan batas memori 700MB (disarankan di mesin ini)
make test           # go test -race ./...
```

`make build` melakukan: `bun install` → `vite build` → gzip aset → `go build`.
Node/npm **tidak** diperlukan; hanya Bun (`/usr/local/bin/bun`) dan hanya saat build.
Server produksi cuma menjalankan binary.

## Pasang sebagai service

```sh
make build-safe && sudo sh deploy/install.sh
systemctl status hopface        # start/stop/restart: systemctl restart hopface
journalctl -u hopface -f        # log
```

Service berjalan sebagai user `hopface` (non-root), `Restart=always`, autostart saat boot,
`MemoryMax=300M`. Data (pengguna, blokir, laporan) ada di `/var/lib/hopface/*.json`.

## Konfigurasi (`/etc/project-app1/`, izin 600)

| Berkas | Isi |
|---|---|
| `hopface.env` | `HOPFACE_BASE_URL`, `HOPFACE_SESSION_SECRET`, `HOPFACE_ADMIN_EMAILS` (dibuat otomatis oleh `install.sh`) |
| `turn.env` | `TURN_KEY_ID`, `TURN_API_TOKEN`, `TURN_TTL` (Cloudflare TURN) |
| `google.env` | `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET` |

Tanpa `google.env`, tidak ada yang bisa login (halaman awal menampilkan pesan "belum dikonfigurasi").
**Jangan** menyalakan `HOPFACE_DEV=1` di server publik: itu membuka login palsu untuk siapa saja.

### Login Google

1. Google Cloud Console → *APIs & Services* → *OAuth consent screen*, lalu *Credentials* → *OAuth client ID* (Web application).
2. *Authorized redirect URI*: `https://DOMAIN/auth/google/callback`.
3. Isi `/etc/project-app1/google.env` langsung di server (jangan ditempel ke chat/tiket):

   ```
   GOOGLE_CLIENT_ID=...
   GOOGLE_CLIENT_SECRET=...
   ```
4. Ubah `HOPFACE_BASE_URL=https://DOMAIN` di `hopface.env`, lalu `systemctl restart hopface`.

Scope yang diminta hanya `openid email profile`. Google **tidak** memberi umur/lokasi pada scope ini,
karena itu umur diisi pengguna (wajib 18+, terkunci setelah diisi) dan negara dipilih pengguna.

## HTTPS lewat Caddy (wajib untuk kamera/mikrofon)

Browser hanya mengizinkan kamera dan mikrofon di HTTPS. Tambahkan blok ke `/etc/caddy/Caddyfile`
(contoh, belum diterapkan):

```
DOMAIN {
	encode zstd gzip
	reverse_proxy 127.0.0.1:8868
}
```

Caddy otomatis meneruskan WebSocket dan mengirim `X-Forwarded-For` (dipakai untuk rate limit per IP,
hanya dipercaya bila koneksi berasal dari loopback).

## Admin

Isi `HOPFACE_ADMIN_EMAILS=email@anda.com` lalu restart. Setelah login dengan akun itu, menu **Admin**
menampilkan laporan beserta 20 pesan teks terakhir, dan tombol blokir/tandai selesai.
Akun yang dilaporkan ≥3 orang berbeda dalam 24 jam diblokir otomatis 24 jam.

## Batasan yang perlu diketahui

- Penyimpanan berkas JSON cukup untuk puluhan ribu akun; di atas itu ganti dengan database.
- Pencocokan di memori satu proses: tidak bisa diskalakan ke beberapa instance tanpa perubahan.
- Umur adalah pernyataan pengguna, bukan verifikasi identitas.
- Teks ToS/Privacy di `web/src/views` adalah draf: **minta tinjauan hukum** sebelum dirilis publik.
