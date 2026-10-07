// Package web membawa hasil build frontend (Vue + Vite) ke dalam binary Go.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// Dist mengembalikan isi folder dist sebagai akar sistem berkas.
func Dist() (fs.FS, error) { return fs.Sub(distFS, "dist") }
