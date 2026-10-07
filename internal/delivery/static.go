package delivery

import (
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

// spaHandler menyajikan hasil build frontend yang di-embed.
//   - Berkas ber-hash di /assets/ di-cache setahun (immutable).
//   - index.html tidak di-cache supaya rilis baru langsung terpakai.
//   - Bila ada berkas .gz hasil build dan klien mendukung gzip, versi itu yang dikirim
//     (kompresi dilakukan saat build, bukan saat request, jadi CPU hemat).
//   - Path tanpa ekstensi yang tidak ada jatuh ke index.html (routing sisi klien).
func spaHandler(fsys fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		if st, err := fs.Stat(fsys, name); err != nil || st.IsDir() {
			if path.Ext(name) != "" {
				http.NotFound(w, r)
				return
			}
			name = "index.html"
		}
		serveFile(w, r, fsys, name)
	})
}

func serveFile(w http.ResponseWriter, r *http.Request, fsys fs.FS, name string) {
	h := w.Header()
	h.Add("Vary", "Accept-Encoding")
	if strings.HasPrefix(name, "assets/") {
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		h.Set("Cache-Control", "no-cache")
	}
	if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
		h.Set("Content-Type", ct)
	}

	open := name
	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		if _, err := fs.Stat(fsys, name+".gz"); err == nil {
			open = name + ".gz"
			h.Set("Content-Encoding", "gzip")
		}
	}
	f, err := fsys.Open(open)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if rs, ok := f.(io.ReadSeeker); ok {
		http.ServeContent(w, r, name, st.ModTime(), rs)
		return
	}
	_, _ = io.Copy(w, f)
}
