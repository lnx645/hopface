package domain

import (
	"errors"
	"testing"
	"time"
)

var now = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

func TestAgeAt(t *testing.T) {
	cases := []struct {
		birth string
		want  int
	}{
		{"2008-10-07", 18}, // ulang tahun hari ini
		{"2008-10-08", 17}, // besok ulang tahun
		{"2000-01-01", 26},
		{"2026-10-07", 0},
	}
	for _, c := range cases {
		b, _ := time.Parse("2006-01-02", c.birth)
		if got := AgeAt(b, now); got != c.want {
			t.Errorf("AgeAt(%s)=%d, want %d", c.birth, got, c.want)
		}
	}
}

func TestApplyProfile(t *testing.T) {
	u := User{ID: "u"}
	if err := u.ApplyProfile("2008-10-08", GenderMale, "id", now); !errors.Is(err, ErrUnderage) {
		t.Fatalf("17 tahun harus ditolak, got %v", err)
	}
	if u.Birthdate != "" {
		t.Fatal("tanggal lahir tidak boleh tersimpan saat ditolak")
	}
	if err := u.ApplyProfile("2000-05-05", GenderFemale, "id", now); err != nil {
		t.Fatal(err)
	}
	if u.Country != "ID" || !u.ProfileComplete() {
		t.Fatalf("profil tidak lengkap: %+v", u)
	}
	// tanggal lahir terkunci: percobaan mengubah ke tanggal lain diabaikan
	if err := u.ApplyProfile("1990-01-01", GenderFemale, "US", now); err != nil {
		t.Fatal(err)
	}
	if u.Birthdate != "2000-05-05" {
		t.Fatalf("tanggal lahir harus terkunci, got %s", u.Birthdate)
	}
	if u.Country != "US" {
		t.Fatal("negara boleh diubah")
	}
	for _, bad := range []struct {
		b string
		g Gender
		c string
	}{{"bukan-tanggal", GenderMale, "ID"}, {"2000-01-01", "x", "ID"}, {"2000-01-01", GenderMale, "INDO"}, {"1900-01-01", GenderMale, "ID"}} {
		v := User{ID: "v"}
		if err := v.ApplyProfile(bad.b, bad.g, bad.c, now); err == nil {
			t.Errorf("seharusnya ditolak: %+v", bad)
		}
	}
}

func TestFilter(t *testing.T) {
	p := Profile{Age: 25, Gender: GenderFemale, Country: "ID"}
	cases := []struct {
		name string
		f    Filter
		want bool
	}{
		{"kosong = bebas", Filter{}, true},
		{"gender cocok", Filter{Gender: GenderFemale}, true},
		{"gender beda", Filter{Gender: GenderMale}, false},
		{"umur dalam rentang", Filter{MinAge: 20, MaxAge: 30}, true},
		{"umur di bawah", Filter{MinAge: 26, MaxAge: 30}, false},
		{"negara beda", Filter{Country: "US"}, false},
		{"negara cocok", Filter{Country: "id"}, true},
	}
	for _, c := range cases {
		if got := c.f.Normalize().Matches(p); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestFilterNormalizeMin18(t *testing.T) {
	f := Filter{MinAge: 5, MaxAge: 500}.Normalize()
	if f.MinAge != MinAge || f.MaxAge != MaxAge {
		t.Fatalf("filter umur harus dibatasi 18..99, got %+v", f)
	}
	f = Filter{MinAge: 40, MaxAge: 20}.Normalize()
	if f.MinAge != 20 || f.MaxAge != 40 {
		t.Fatalf("rentang terbalik harus ditukar, got %+v", f)
	}
}
