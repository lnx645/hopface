// Program genstickers membuat paket stiker/kelakar klasik ala emoticon
// forum tahun 2013: smiley kuning mengkilap 3D berukuran 64x64 piksel.
//
// Seluruh gambar digambar per-piksel secara prosedural (tanpa aset eksternal
// dan tanpa dependensi di luar pustaka standar Go), lalu disupersampling 4x4
// agar tepi bentuk tetap halus.
//
// Pemakaian:
//
//	go run ./cmd/genstickers [direktori-tujuan]
//
// Default direktori tujuan: web/public/img/sticker
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

// Geometri dasar kanvas.
const (
	sisi    = 64   // sisi kanvas (piksel)
	tingkat = 4    // supersampling: tingkat x tingkat sampel per piksel
	pusatX  = 32.0 // titik pusat wajah
	pusatY  = 32.0
	rTepi   = 30.0 // jari-jari luar wajah (termasuk kontur cokelat)
	rWajah  = 28.5 // jari-jari bidang wajah (gradasi kuning)
)

// Palet warna klasik.
var (
	wPusat    = warna{255, 246, 160, 255} // #fff6a0, tengah gradasi
	wTepi     = warna{242, 178, 0, 255}   // #f2b200, tepi gradasi
	wKontur   = warna{74, 44, 6, 255}     // cokelat gelap untuk garis tepi
	wFitur    = warna{24, 19, 14, 255}    // hitam untuk mata/mulut/alis
	wPutih    = warna{255, 255, 255, 255}
	wMerah    = warna{232, 45, 60, 255} // hati dan bibir
	wMerahTua = warna{165, 25, 40, 255} // garis bibir / lekukan lidah
	wLidah    = warna{232, 78, 80, 255}
	wBiru     = warna{70, 160, 240, 255} // air mata
	wKacamata = warna{18, 16, 16, 255}   // lensa kacamata hitam
)

// warna adalah warna lurus (non-premultiplied) dengan komponen 0..255.
type warna struct{ r, g, b, a float64 }

// campur menghitung pencampuran dua warna; t=0 menghasilkan c1, t=1 menghasilkan c2.
func campur(c1, c2 warna, t float64) warna {
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	return warna{
		r: c1.r + (c2.r-c1.r)*t,
		g: c1.g + (c2.g-c1.g)*t,
		b: c1.b + (c2.b-c1.b)*t,
		a: c1.a + (c2.a-c1.a)*t,
	}
}

// kegelapan mengalikan komponen terang warna (untuk bayangan halus).
func kegelapan(c warna, f float64) warna {
	return warna{r: c.r * f, g: c.g * f, b: c.b * f, a: c.a}
}

// ---------------------------------------------------------------------------
// Geometri per-piksel
// ---------------------------------------------------------------------------

// jarakEllipse mengembalikan jarak ternormalisasi dari titik (x,y) terhadap
// elips; hasil <= 1 berarti titik berada di dalam elips.
func jarakEllipse(x, y, cx, cy, rx, ry, rot float64) float64 {
	c, s := math.Cos(rot), math.Sin(rot)
	dx, dy := x-cx, y-cy
	u := (dx*c + dy*s) / rx
	v := (-dx*s + dy*c) / ry
	return math.Hypot(u, v)
}

// jarakSegmen menghitung jarak titik (x,y) terhadap garis dari A ke B.
func jarakSegmen(x, y, x1, y1, x2, y2 float64) float64 {
	dx, dy := x2-x1, y2-y1
	panjang2 := dx*dx + dy*dy
	if panjang2 == 0 {
		return math.Hypot(x-x1, y-y1)
	}
	t := ((x-x1)*dx + (y-y1)*dy) / panjang2
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	return math.Hypot(x-(x1+t*dx), y-(y1+t*dy))
}

// tandaSegitiga menghitung luas berarah dua kali untuk uji titik dalam segitiga.
func tandaSegitiga(x1, y1, x2, y2, x3, y3 float64) float64 {
	return (x1-x3)*(y2-y3) - (x2-x3)*(y1-y3)
}

// ---------------------------------------------------------------------------
// Fitur wajah (digambar per-piksel di atas warna dasar)
// ---------------------------------------------------------------------------

// fitur adalah satu bentuk fitur wajah; mengembalikan warnanya bila titik
// berada di dalam bentuk.
type fitur func(x, y float64) (warna, bool)

// fiturEllipse membuat fitur berbentuk elips (rotasi dalam radian).
func fiturEllipse(cx, cy, rx, ry, rot float64, w warna) fitur {
	return func(x, y float64) (warna, bool) {
		if jarakEllipse(x, y, cx, cy, rx, ry, rot) <= 1 {
			return w, true
		}
		return warna{}, false
	}
}

// fiturGaris membuat fitur berupa garis tebal.
func fiturGaris(x1, y1, x2, y2, tebal float64, w warna) fitur {
	return func(x, y float64) (warna, bool) {
		if jarakSegmen(x, y, x1, y1, x2, y2) <= tebal {
			return w, true
		}
		return warna{}, false
	}
}

// fiturPita membuat fitur berupa pita parabola y = cy + k*(x-cx)^2 dengan
// lebar horizontal dan ketebalan tegak tertentu. K>0 menghasilkan busur menyenggol
// ke atas (muka cemberut/mata terpejam), k<0 menghasilkan senyum.
func fiturPita(cx, cy, k, lebar, tebal float64, w warna) fitur {
	return func(x, y float64) (warna, bool) {
		dx := x - cx
		if math.Abs(dx) > lebar {
			return warna{}, false
		}
		if math.Abs(y-(cy+k*dx*dx)) <= tebal {
			return w, true
		}
		return warna{}, false
	}
}

// fiturSegitiga membuat fitur berbentuk segitiga (dipakai untuk badan hati).
func fiturSegitiga(x1, y1, x2, y2, x3, y3 float64, w warna) fitur {
	return func(x, y float64) (warna, bool) {
		d1 := tandaSegitiga(x, y, x1, y1, x2, y2)
		d2 := tandaSegitiga(x, y, x2, y2, x3, y3)
		d3 := tandaSegitiga(x, y, x3, y3, x1, y1)
		negatif := d1 < 0 || d2 < 0 || d3 < 0
		positif := d1 > 0 || d2 > 0 || d3 > 0
		if !(negatif && positif) {
			return w, true
		}
		return warna{}, false
	}
}

// fiturGabung menyatukan beberapa sub-bentuk berwarna sama (union).
func fiturGabung(fs ...fitur) fitur {
	return func(x, y float64) (warna, bool) {
		for _, f := range fs {
			if w, ok := f(x, y); ok {
				return w, true
			}
		}
		return warna{}, false
	}
}

// fiturHati membuat bentuk hati: dua lingkaran atas + segitiga terbalik.
func fiturHati(cx, cy float64, w warna) fitur {
	kiri := fiturEllipse(cx-3.6, cy-2.4, 4.2, 4.2, 0, w)
	kanan := fiturEllipse(cx+3.6, cy-2.4, 4.2, 4.2, 0, w)
	badan := fiturSegitiga(cx-7.6, cy-1.5, cx+7.6, cy-1.5, cx, cy+7.5, w)
	return fiturGabung(kiri, kanan, badan)
}

// fiturTetes membuat bentuk tetesan air mata (kerucut di atas + bulatan di bawah).
func fiturTetes(cx, puncak, tinggi, rad float64, w warna) fitur {
	return func(x, y float64) (warna, bool) {
		pusat := puncak + tinggi
		if math.Hypot(x-cx, y-pusat) <= rad {
			return w, true
		}
		if y >= puncak && y <= pusat && math.Abs(x-cx) <= rad*(y-puncak)/tinggi {
			return w, true
		}
		return warna{}, false
	}
}

// fiturZ membuat huruf "z" dari tiga goresan (garis atas, diagonal, garis bawah).
func fiturZ(x0, y0, uk, tebal float64, w warna) fitur {
	atas := fiturGaris(x0, y0, x0+uk, y0, tebal, w)
	diag := fiturGaris(x0+uk, y0, x0, y0+uk, tebal, w)
	bawah := fiturGaris(x0, y0+uk, x0+uk, y0+uk, tebal, w)
	return fiturGabung(atas, diag, bawah)
}

// ---------------------------------------------------------------------------
// Wajah dasar: lingkaran kuning gradasi + kilap 3D + kontur
// ---------------------------------------------------------------------------

// dasar mengembalikan warna wajah pada titik (x,y); ok=false bila di luar wajah.
func dasar(x, y float64) (warna, bool) {
	dx := x - pusatX
	dy := y - pusatY
	d := math.Hypot(dx, dy)
	if d > rTepi {
		return warna{}, false
	}
	// Kontur cokelat gelap tipis di sekeliling tepi wajah.
	if d > rWajah {
		return wKontur, true
	}
	// Gradasi radial kuning: tengah cerah -> tepi jenuh.
	t := d / rWajah
	c := campur(wPusat, wTepi, 0.65*t+0.35*t*t)
	// Bayangan halus di arah kanan-bawah (kesan bola 3D).
	s := (dx*0.7 + dy*0.7) / rWajah
	if s > 0 {
		if s > 1 {
			s = 1
		}
		c = kegelapan(c, 1-0.16*s*s)
	}
	// Kilap oval putih semi transparan di kiri atas (kesan mengkilap).
	q := jarakEllipse(x, y, 23, 17, 11, 6, 0.6)
	if q < 1 {
		c = campur(c, wPutih, 0.72*math.Pow(1-q, 1.4))
	}
	// Bintik kilap kecil yang lebih tajam.
	q = jarakEllipse(x, y, 21, 14, 4, 2.6, 0.55)
	if q < 1 {
		c = campur(c, wPutih, 0.55*math.Pow(1-q, 1.1))
	}
	return c, true
}

// gambar mengambil warna akhir satu sampel piksel untuk varian tertentu.
func gambar(v varian, x, y float64) warna {
	c, ok := dasar(x, y)
	if !ok {
		return warna{}
	}
	// Fitur digambar berurutan; yang belakang menimpa yang di depan.
	for _, f := range v.fitur {
		if fc, hit := f(x, y); hit {
			if fc.a >= 255 {
				c = fc
			} else {
				c = campur(c, fc, fc.a/255)
				c.a = 255 // hasil komposit di atas dasar yang selalu buram
			}
		}
	}
	return c
}

// ---------------------------------------------------------------------------
// Potongan fitur yang dipakai bersama antar varian
// ---------------------------------------------------------------------------

func mataStandar(cx float64) fitur {
	return fiturEllipse(cx, 25, 4.5, 6, 0, wFitur)
}

func senyumKlasik() fitur {
	// Titik terendah di tengah (y=42), ujung naik -> senyum.
	return fiturPita(32, 42, -0.05, 12.5, 2.4, wFitur)
}

func mulutSedih() fitur {
	// Titik tertinggi di tengah (y=45), ujung turun -> cemberut.
	return fiturPita(32, 45, 0.05, 11, 2.2, wFitur)
}

func mataTerpejamKetawa(cx float64) fitur {
	// Busur menyenggol ke atas (mata tertutup ketawa).
	return fiturPita(cx, 24, 0.06, 6.5, 1.9, wFitur)
}

func mataTerpejamLembut(cx float64) fitur {
	// Cekung lembut (mata terpejam).
	return fiturPita(cx, 27, -0.05, 6, 1.8, wFitur)
}

// ---------------------------------------------------------------------------
// 12 varian stiker
// ---------------------------------------------------------------------------

// varian adalah satu paket stiker beserta daftar fitur wajahnya.
type varian struct {
	nama  string
	fitur []fitur
}

func semuaVarian() []varian {
	return []varian{
		{"happy", []fitur{
			mataStandar(23), mataStandar(41), senyumKlasik(),
		}},
		{"sad", []fitur{
			mataStandar(23), mataStandar(41), mulutSedih(),
		}},
		{"laugh", []fitur{
			mataTerpejamKetawa(23), mataTerpejamKetawa(41),
			fiturEllipse(32, 44, 12, 9, 0, wFitur), // mulut terbuka besar
		}},
		{"wink", []fitur{
			mataStandar(23),
			fiturPita(41, 27, -0.05, 6, 1.7, wFitur), // satu mata sedikit tertutup
			senyumKlasik(),
		}},
		{"tongue", []fitur{
			mataStandar(23), mataStandar(41), senyumKlasik(),
			fiturEllipse(32, 44, 6.5, 6, 0, wLidah),    // lidah merah
			fiturGaris(32, 43, 32, 49, 0.9, wMerahTua), // lekukan lidah
		}},
		{"cool", []fitur{
			fiturGaris(13, 24, 6.5, 27, 1.8, wKacamata),    // tangan kiri
			fiturGaris(51, 24, 57.5, 27, 1.8, wKacamata),   // tangan kanan
			fiturGaris(30.5, 25, 33.5, 25, 2.2, wKacamata), // jembatan hidung
			fiturEllipse(22, 26, 9, 8.5, 0, wKacamata),     // lensa kiri
			fiturEllipse(42, 26, 9, 8.5, 0, wKacamata),     // lensa kanan
			fiturEllipse(17, 22, 3.2, 1.5, -0.5, warna{255, 255, 255, 110}),
			fiturEllipse(37, 22, 3.2, 1.5, -0.5, warna{255, 255, 255, 110}),
		}},
		{"surprise", []fitur{
			fiturEllipse(23, 25, 5.5, 6.5, 0, wFitur), // mata bulat
			fiturEllipse(41, 25, 5.5, 6.5, 0, wFitur),
			fiturEllipse(32, 44, 6, 7, 0, wFitur), // mulut "o"
		}},
		{"cry", []fitur{
			fiturGaris(16, 25, 27, 20.5, 2.4, wFitur), // alis sedih kiri
			fiturGaris(48, 25, 37, 20.5, 2.4, wFitur), // alis sedih kanan
			fiturEllipse(23, 28, 4.5, 5.5, 0, wFitur),
			fiturEllipse(41, 28, 4.5, 5.5, 0, wFitur),
			mulutSedih(),
			fiturTetes(19, 33, 7, 4, wBiru), // air mata di pipi kiri
			fiturEllipse(17.6, 37.5, 1.1, 1.8, 0, warna{255, 255, 255, 150}),
		}},
		{"angry", []fitur{
			fiturGaris(16, 17, 28, 24, 2.6, wFitur), // alis miring kiri
			fiturGaris(48, 17, 36, 24, 2.6, wFitur), // alis miring kanan
			fiturEllipse(23, 29, 4.2, 4.6, 0, wFitur),
			fiturEllipse(41, 29, 4.2, 4.6, 0, wFitur),
			fiturPita(32, 42, 0.06, 11, 2.2, wFitur), // mulut cemberut
		}},
		{"kiss", []fitur{
			mataTerpejamLembut(23), mataTerpejamLembut(41),
			fiturEllipse(32, 42, 5.5, 4, 0, wMerah),      // bibir merah kecil
			fiturGaris(26.5, 42, 37.5, 42, 1, wMerahTua), // belahan bibir
		}},
		{"sleepy", []fitur{
			fiturGaris(17, 27, 28, 27, 1.6, wFitur),   // mata garis kiri
			fiturGaris(36, 27, 47, 27, 1.6, wFitur),   // mata garis kanan
			fiturEllipse(32, 44, 3.5, 2.6, 0, wFitur), // mulut kecil
			fiturZ(30, 14, 8, 1.4, wFitur),            // huruf "z" besar
			fiturZ(40, 8, 5, 1.2, wFitur),             // huruf "z" kecil
		}},
		{"love", []fitur{
			fiturHati(23, 25, wMerah), // mata hati merah
			fiturHati(41, 25, wMerah),
			senyumKlasik(),
		}},
	}
}

// ---------------------------------------------------------------------------
// Render dan penulisan berkas
// ---------------------------------------------------------------------------

// render menggambar satu varian ke kanvas RGBA 64x64 dengan supersampling.
func render(v varian) *image.RGBA {
	kanvas := image.NewRGBA(image.Rect(0, 0, sisi, sisi))
	n := float64(tingkat * tingkat)
	for py := 0; py < sisi; py++ {
		for px := 0; px < sisi; px++ {
			var sr, sg, sb, sa float64
			for sy := 0; sy < tingkat; sy++ {
				for sx := 0; sx < tingkat; sx++ {
					x := float64(px) + (float64(sx)+0.5)/float64(tingkat)
					y := float64(py) + (float64(sy)+0.5)/float64(tingkat)
					c := gambar(v, x, y)
					if c.a <= 0 {
						continue
					}
					// Akumulasi premultiplied agar perataan sampel benar.
					sr += c.r * c.a / 255
					sg += c.g * c.a / 255
					sb += c.b * c.a / 255
					sa += c.a
				}
			}
			if sa <= 0 {
				continue // piksel transparan
			}
			kanvas.SetRGBA(px, py, color.RGBA{
				R: uint8(sr/n + 0.5),
				G: uint8(sg/n + 0.5),
				B: uint8(sb/n + 0.5),
				A: uint8(sa/n + 0.5),
			})
		}
	}
	return kanvas
}

func main() {
	tujuan := "web/public/img/sticker"
	if len(os.Args) > 1 {
		tujuan = os.Args[1]
	}
	if err := os.MkdirAll(tujuan, 0o755); err != nil {
		gagal("membuat direktori %s: %v", tujuan, err)
	}

	daftar := semuaVarian()
	fmt.Printf("Menulis %d stiker ke %s\n", len(daftar), tujuan)
	for _, v := range daftar {
		// Kompresi maksimal supaya tiap berkas PNG tetap kecil.
		kanvas := render(v)
		enc := png.Encoder{CompressionLevel: png.BestCompression}
		jalur := filepath.Join(tujuan, v.nama+".png")
		f, err := os.Create(jalur)
		if err != nil {
			gagal("membuat %s: %v", jalur, err)
		}
		if err := enc.Encode(f, kanvas); err != nil {
			f.Close()
			gagal("menulis %s: %v", jalur, err)
		}
		if err := f.Close(); err != nil {
			gagal("menutup %s: %v", jalur, err)
		}
		info, err := os.Stat(jalur)
		if err != nil {
			gagal("membaca ukuran %s: %v", jalur, err)
		}
		fmt.Printf("  %-12s %6d B\n", v.nama+".png", info.Size())
	}
}

// gagal mencetak pesan galat lalu keluar dengan kode 1.
func gagal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "genstickers: "+format+"\n", args...)
	os.Exit(1)
}
