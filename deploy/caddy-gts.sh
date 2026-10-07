#!/bin/bash
# Pasang Google Public CA (gratis, publik tepercaya) sebagai penerbit sertifikat untuk hopface.2bd.net.
# Dipakai saat Let's Encrypt kena batas mingguan domain bersama (2bd.net) dan ZeroSSL lambat.
#
# Alur aman:
#   1. cadangkan Caddyfile
#   2. sisipkan blok `tls { issuer acme ... }` HANYA ke blok hopface (AnyDown tidak disentuh)
#   3. `caddy validate`; bila gagal, kembalikan cadangan dan berhenti
#   4. `systemctl reload caddy` (graceful, bukan restart)
#
# Kunci EAB hanya berlaku sekali: setelah akun ACME terdaftar, kunci itu tidak bisa dipakai lagi,
# jadi menaruhnya di Caddyfile tidak berbahaya setelah sertifikat terbit. Meski begitu, dibaca
# dengan `read -s` agar tidak tampil di layar atau riwayat shell.
#
# Variabel opsional (untuk pengujian): CADDYFILE, NO_RELOAD=1, EAB_KID, EAB_HMAC
set -euo pipefail

CADDYFILE=${CADDYFILE:-/etc/caddy/Caddyfile}
HOST=${HOST_NAME:-hopface.2bd.net}
export HOME=${HOME:-/root} XDG_CONFIG_HOME=${XDG_CONFIG_HOME:-/root/.config}

[ -f "$CADDYFILE" ] || { echo "Caddyfile tidak ditemukan: $CADDYFILE" >&2; exit 1; }
grep -q "^$HOST {" "$CADDYFILE" || { echo "blok '$HOST {' tidak ada di $CADDYFILE" >&2; exit 1; }
if grep -q "dv.acme-v02.api.pki.goog" "$CADDYFILE"; then
  echo "Google Public CA sudah dikonfigurasi di $CADDYFILE. Tidak ada yang diubah."; exit 0
fi

if [ -z "${EAB_KID:-}" ]; then read -r -p "EAB key ID (keyId): " EAB_KID; fi
if [ -z "${EAB_HMAC:-}" ]; then read -r -s -p "EAB HMAC (b64MacKey, tidak ditampilkan): " EAB_HMAC; echo; fi
# Hanya karakter yang aman untuk Caddyfile (base64/base64url dan ID heksa/alfanumerik)
case "$EAB_KID$EAB_HMAC" in *[!A-Za-z0-9_=+/-]*) echo "kunci mengandung karakter tidak valid" >&2; exit 1;; esac
[ -n "$EAB_KID" ] && [ -n "$EAB_HMAC" ] || { echo "keyId dan HMAC wajib diisi" >&2; exit 1; }

# Kunci asli dari Google panjang. Nilai pendek hampir pasti salah tempel (mis. teks lain, atau
# keyId dan HMAC tertukar). Menolaknya di sini mencegah Caddy mengulang kegagalan tiap beberapa
# menit sambil menutup jalur cadangan Let's Encrypt/ZeroSSL.
if [ "${#EAB_KID}" -lt 10 ]; then echo "keyId terlalu pendek (${#EAB_KID} karakter). Salin nilai 'keyId' lengkap dari hasil gcloud." >&2; exit 1; fi
if [ "${#EAB_HMAC}" -lt 30 ]; then echo "HMAC terlalu pendek (${#EAB_HMAC} karakter). Salin nilai 'b64MacKey' lengkap dari hasil gcloud." >&2; exit 1; fi
if [ "$EAB_KID" = "$EAB_HMAC" ]; then echo "keyId dan HMAC sama persis; kemungkinan salah tempel." >&2; exit 1; fi
# Caddy membaca HMAC sebagai base64url; periksa sebelum menyentuh Caddyfile.
if ! EAB_HMAC="$EAB_HMAC" python3 -c "
import os,base64,sys
m=os.environ['EAB_HMAC'].rstrip('=')
try: base64.urlsafe_b64decode(m+'='*(-len(m)%4))
except Exception: sys.exit(1)"; then
  echo "HMAC bukan base64url yang valid. Pastikan Anda menyalin 'b64MacKey' utuh, tanpa spasi atau tanda kutip." >&2; exit 1
fi
# Caddy memakai base64url TANPA padding '='; buang padding bila ada.
EAB_HMAC="${EAB_HMAC%%=*}"

BACKUP="$CADDYFILE.bak-gts-$(date +%s)"
cp -a "$CADDYFILE" "$BACKUP"
echo "cadangan: $BACKUP"

EAB_KID="$EAB_KID" EAB_HMAC="$EAB_HMAC" CADDYFILE="$CADDYFILE" HOST="$HOST" python3 - <<'PY'
import os, re
p, host = os.environ["CADDYFILE"], os.environ["HOST"]
s = open(p).read()
anchor = host + " {\n"
i = s.index("\n" + anchor) + 1 if ("\n" + anchor) in s else 0
head = s[: i + len(anchor)]
tls = (
    "\ttls {\n"
    "\t\tissuer acme {\n"
    "\t\t\tdir https://dv.acme-v02.api.pki.goog/directory\n"
    f"\t\t\teab {os.environ['EAB_KID']} {os.environ['EAB_HMAC']}\n"
    "\t\t}\n"
    "\t}\n\n"
)
open(p, "w").write(head + tls + s[i + len(anchor):])
PY

if ! caddy validate --config "$CADDYFILE" --adapter caddyfile >/tmp/caddy-validate.log 2>&1; then
  echo "KONFIGURASI TIDAK VALID. Mengembalikan cadangan." >&2
  grep -v '"level":"warn"' /tmp/caddy-validate.log | tail -5 >&2
  cp -a "$BACKUP" "$CADDYFILE"
  exit 1
fi
echo "konfigurasi valid"

if [ "${NO_RELOAD:-}" = "1" ]; then echo "NO_RELOAD=1: tidak me-reload caddy."; exit 0; fi
systemctl reload caddy
echo "caddy di-reload (graceful). Sertifikat diminta dari Google Public CA."
echo "Pantau: journalctl -u caddy -f | grep -i hopface"
