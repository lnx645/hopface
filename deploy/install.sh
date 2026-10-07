#!/bin/sh
# Pasang Hopface sebagai service systemd. Idempoten: aman dijalankan ulang untuk memperbarui binary.
# Jalankan dari akar proyek sebagai root setelah `make build`.
set -eu

[ "$(id -u)" -eq 0 ] || { echo "harus dijalankan sebagai root" >&2; exit 1; }
[ -x bin/hopface ] || { echo "bin/hopface belum ada; jalankan 'make build' dulu" >&2; exit 1; }

# user sistem non-root tanpa shell
id hopface >/dev/null 2>&1 || useradd --system --home /var/lib/hopface --shell /usr/sbin/nologin hopface

install -d -o hopface -g hopface -m 700 /var/lib/hopface
install -d -m 700 /etc/project-app1
install -d -m 755 /opt/hopface
install -m 755 bin/hopface /opt/hopface/hopface

# konfigurasi: dibuat sekali saja agar rahasia yang sudah ada tidak tertimpa
if [ ! -f /etc/project-app1/hopface.env ]; then
  umask 077
  secret=$(head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n')
  sed "s/GANTI_DENGAN_ACAK/$secret/" deploy/hopface.env.example > /etc/project-app1/hopface.env
  echo "dibuat /etc/project-app1/hopface.env (kunci sesi acak)"
fi
chmod 600 /etc/project-app1/hopface.env

install -m 644 deploy/hopface.service /etc/systemd/system/hopface.service
systemctl daemon-reload
systemctl enable hopface.service
systemctl restart hopface.service
sleep 1
systemctl --no-pager --lines=5 status hopface.service || true
