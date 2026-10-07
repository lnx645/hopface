# Hopface — build frontend (Bun + Vite), kompres aset, lalu build binary Go yang meng-embed hasilnya.
# Catatan mesin ini: HOME kosong di shell, Go 1.19, RAM sempit. Build dibatasi dengan `make build-safe`.

export HOME ?= /root
BIN      := bin/hopface
WEB      := web
GOFLAGS  ?= -p=1

.PHONY: all web gzip build test vet run dev build-safe install clean

all: build

# 1. Frontend -> web/dist
web:
	cd $(WEB) && bun install --frozen-lockfile && bunx --bun vite build

# 2. Kompres aset teks saat build (bukan saat request) supaya server hemat CPU
gzip: web
	find $(WEB)/dist -type f \( -name '*.js' -o -name '*.css' -o -name '*.html' -o -name '*.svg' \) ! -name '*.gz' -exec gzip -9 -k -f {} \;
	touch $(WEB)/dist/.gitkeep

# 3. Binary tunggal (dist ikut di-embed)
build: gzip
	CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' -o $(BIN) ./cmd/hopface

vet:
	go vet ./...

test:
	go test -race -count=1 ./...

# Jalankan lokal dalam mode dev (login palsu aktif) di port 8868
run: build
	HOPFACE_DEV=1 HOPFACE_DATA=./data $(BIN)

# Frontend hot-reload di :5173 (API diteruskan ke :8868)
dev:
	cd $(WEB) && bunx --bun vite

# Build dengan batas memori 700MB supaya proses lain (mis. OpenCode) tidak terancam OOM
build-safe:
	systemd-run --quiet --wait --pipe --collect -p MemoryMax=700M -p Nice=10 \
	  -E HOME=$(HOME) -E GOFLAGS=$(GOFLAGS) -E PATH="$$PATH" --working-directory="$$PWD" \
	  /usr/bin/make build

# Pasang sebagai service systemd (butuh root). Lihat deploy/README.md.
install: build
	sh deploy/install.sh

clean:
	rm -rf $(BIN) $(WEB)/dist/* $(WEB)/node_modules
	touch $(WEB)/dist/.gitkeep
