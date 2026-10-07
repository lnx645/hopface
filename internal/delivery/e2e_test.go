package delivery

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	_ "image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gorilla/websocket"

	"hopface/internal/infra"
	"hopface/internal/usecase"
)

// ---- pengganti dependensi eksternal ----

type fakeIdentity struct{ id infra.Identity }

func (fakeIdentity) Configured() bool { return true }
func (fakeIdentity) AuthURL(state string) string {
	return "https://accounts.example/auth?state=" + state
}
func (f fakeIdentity) Exchange(context.Context, string) (infra.Identity, error) {
	return f.id, nil
}

type fakeICE struct{}

func (fakeICE) IceServers(context.Context) (json.RawMessage, error) {
	return json.RawMessage(`{"iceServers":[{"urls":["stun:x"]}]}`), nil
}

func gz(s string) []byte {
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	_, _ = w.Write([]byte(s))
	_ = w.Close()
	return b.Bytes()
}

type env struct {
	srv      *httptest.Server
	mod      *usecase.Moderation
	lob      *usecase.Lobby
	repo     *infra.ModerationStore
	accounts *usecase.Accounts
	signer   *infra.Signer
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	users, _ := infra.NewUserStore(dir)
	repo, _ := infra.NewModerationStore(dir)
	signer, _ := infra.NewSigner([]byte("rahasia-uji-yang-cukup-panjang"))
	avatarStore, _ := infra.NewAvatarStore(dir)
	mod := usecase.NewModeration(repo, nil)
	mm := usecase.NewMatchmaker(10*time.Second, 30*time.Second, nil)
	lob := usecase.NewLobby(mm, mod, nil)
	mod.OnBan = lob.Kick
	static := fstest.MapFS{
		"index.html":           {Data: []byte("<html>SPA</html>")},
		"assets/app-abc.js":    {Data: []byte("console.log(1)")},
		"assets/app-abc.js.gz": {Data: gz("console.log(1)")},
		"avatar-default.svg":   {Data: []byte("<svg/>")},
	}
	accounts := usecase.NewAccounts(users, avatarStore, nil)
	h := New(Deps{
		Cfg:      Config{Dev: true, AdminEmails: map[string]bool{"admin@example.com": true}},
		Accounts: accounts, Moderation: mod, Lobby: lob,
		Signer: signer, Identity: fakeIdentity{infra.Identity{Sub: "1", Email: "g@example.com", EmailVerified: true, Name: "Gina Test"}},
		ICE: fakeICE{}, Static: static, Avatar: avatarStore,
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &env{srv: srv, mod: mod, lob: lob, repo: repo, accounts: accounts, signer: signer}
}

type client struct {
	t    *testing.T
	e    *env
	http *http.Client
}

func (e *env) client(t *testing.T) *client {
	jar, _ := cookiejar.New(nil)
	return &client{t: t, e: e, http: &http.Client{Jar: jar}}
}

func (c *client) do(method, path, body string) (int, string) {
	c.t.Helper()
	req, _ := http.NewRequest(method, c.e.srv.URL+path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func (c *client) login(email string) {
	c.t.Helper()
	if code, _ := c.do("GET", "/auth/dev?email="+email, ""); code != 200 {
		c.t.Fatalf("login %s: %d", email, code)
	}
}

func (c *client) profile(birth, gender, country string) (int, string) {
	return c.do("POST", "/api/profile", `{"birthdate":"`+birth+`","gender":"`+gender+`","country":"`+country+`"}`)
}

func (c *client) ready(email, gender, country string) {
	c.t.Helper()
	c.login(email)
	if code, body := c.profile("1995-03-04", gender, country); code != 200 {
		c.t.Fatalf("profil %s: %d %s", email, code, body)
	}
}

type wsc struct {
	t  *testing.T
	ws *websocket.Conn
}

func (c *client) dial() *wsc {
	c.t.Helper()
	u := "ws" + strings.TrimPrefix(c.e.srv.URL, "http") + "/ws"
	hdr := http.Header{}
	// gorilla memeriksa Origin == Host; klien non-browser tanpa Origin diizinkan, tapi cookie perlu dikirim.
	base, _ := http.NewRequest("GET", c.e.srv.URL, nil)
	var cs []string
	for _, ck := range c.http.Jar.Cookies(base.URL) {
		cs = append(cs, ck.Name+"="+ck.Value)
	}
	hdr.Set("Cookie", strings.Join(cs, "; "))
	ws, res, err := websocket.DefaultDialer.Dial(u, hdr)
	if err != nil {
		code := 0
		if res != nil {
			code = res.StatusCode
		}
		c.t.Fatalf("dial ws: %v (status %d)", err, code)
	}
	c.t.Cleanup(func() { ws.Close() })
	return &wsc{t: c.t, ws: ws}
}

func (w *wsc) send(v interface{}) {
	w.t.Helper()
	if err := w.ws.WriteJSON(v); err != nil {
		w.t.Fatal(err)
	}
}

// expect membaca pesan sampai menemukan tipe yang diminta (melewati state, dll).
func (w *wsc) expect(typ string) usecase.Event {
	w.t.Helper()
	_ = w.ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		var e usecase.Event
		if err := w.ws.ReadJSON(&e); err != nil {
			w.t.Fatalf("menunggu %q: %v", typ, err)
		}
		if e.T == typ {
			return e
		}
	}
}

func (w *wsc) expectState(state string) {
	w.t.Helper()
	_ = w.ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		var e usecase.Event
		if err := w.ws.ReadJSON(&e); err != nil {
			w.t.Fatalf("menunggu state %q: %v", state, err)
		}
		if e.T == "state" && e.State == state {
			return
		}
	}
}

// ---- tes ----

func TestWSRequiresLoginAndProfile(t *testing.T) {
	e := newEnv(t)
	c := e.client(t)
	if code, _ := c.do("GET", "/ws", ""); code != http.StatusUnauthorized {
		t.Fatalf("tanpa login harus 401, got %d", code)
	}
	c.login("a@example.com") // login tapi profil belum lengkap
	if code, _ := c.do("GET", "/ws", ""); code != http.StatusForbidden {
		t.Fatalf("profil belum lengkap harus 403, got %d", code)
	}
	if code, _ := c.do("GET", "/api/ice", ""); code != http.StatusForbidden {
		t.Fatalf("ice tanpa profil harus 403, got %d", code)
	}
}

func TestProfileAgeGate(t *testing.T) {
	e := newEnv(t)
	c := e.client(t)
	c.login("kid@example.com")
	year := time.Now().Year() - 17
	code, body := c.profile(time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC).Format("2006-01-02"), "male", "ID")
	if code != http.StatusForbidden || !strings.Contains(body, "underage") {
		t.Fatalf("17 tahun harus ditolak: %d %s", code, body)
	}
	_, me := c.do("GET", "/api/me", "")
	if !strings.Contains(me, `"profileComplete":false`) {
		t.Fatalf("profil tidak boleh lengkap: %s", me)
	}
	if code, _ := c.profile("1990-01-01", "female", "ID"); code != 200 {
		t.Fatalf("dewasa harus diterima, got %d", code)
	}
	// setelah terisi, tanggal lahir terkunci walau dikirim lagi
	c.profile("2005-01-01", "female", "US")
	_, me = c.do("GET", "/api/me", "")
	if !strings.Contains(me, `"birthdate":"1990-01-01"`) || !strings.Contains(me, `"country":"US"`) {
		t.Fatalf("birthdate harus terkunci, negara boleh berubah: %s", me)
	}
}

func TestFullMatchFlowOverWebSocket(t *testing.T) {
	e := newEnv(t)
	ca, cb := e.client(t), e.client(t)
	ca.ready("a@example.com", "male", "ID")
	cb.ready("b@example.com", "female", "ID")
	a, b := ca.dial(), cb.dial()

	a.send(map[string]interface{}{"t": "start", "filter": map[string]interface{}{"gender": "female"}})
	a.expectState("searching")
	b.send(map[string]interface{}{"t": "start", "filter": map[string]interface{}{"gender": "male", "minAge": 20, "maxAge": 40}})

	ma, mb := a.expect("matched"), b.expect("matched")
	if ma.Role != "caller" || mb.Role != "callee" || ma.Peer.Name != "b" || mb.Peer.Name != "a" {
		t.Fatalf("matched salah: %+v / %+v", ma, mb)
	}
	if ma.Peer.Age < 18 || ma.Peer.Country != "ID" {
		t.Fatalf("profil pasangan harus berisi umur dan negara: %+v", ma.Peer)
	}

	a.send(map[string]interface{}{"t": "chat", "text": "halo!"})
	if got := b.expect("chat"); got.Text != "halo!" {
		t.Fatalf("chat: %+v", got)
	}
	a.send(map[string]interface{}{"t": "signal", "data": map[string]string{"sdp": "offer"}})
	if got := b.expect("signal"); !strings.Contains(string(got.Data), "offer") {
		t.Fatalf("signal: %s", got.Data)
	}

	// B skip -> A otomatis kembali mencari (tidak perlu menekan apa pun)
	b.send(map[string]string{"t": "next"})
	a.expect("partner_left")
	a.expectState("searching")

	// A menekan Stop -> idle
	a.send(map[string]string{"t": "stop"})
	a.expectState("idle")
}

func TestReportAndBanKicksConnection(t *testing.T) {
	e := newEnv(t)
	bad := e.client(t)
	bad.ready("bad@example.com", "male", "ID")
	wb := bad.dial()
	wb.send(map[string]string{"t": "start"})
	wb.expectState("searching")

	for _, em := range []string{"r1@example.com", "r2@example.com", "r3@example.com"} {
		r := e.client(t)
		r.ready(em, "female", "ID")
		wr := r.dial()
		wr.send(map[string]string{"t": "start"})
		wr.expect("matched")
		wb.expect("matched")
		wr.send(map[string]string{"t": "report", "reason": "kasar"})
		wr.expect("reported")
	}
	// setelah 3 pelapor berbeda, bad diblokir dan koneksinya diputus
	er := wb.expect("error")
	if er.Error != "banned" {
		t.Fatalf("harus 'banned', got %q", er.Error)
	}
	if code, body := bad.do("GET", "/api/me", ""); code != 200 || !strings.Contains(body, `"banned":true`) {
		t.Fatalf("me harus menandai banned: %d %s", code, body)
	}
	if code, _ := bad.do("GET", "/api/ice", ""); code != http.StatusForbidden {
		t.Fatalf("pengguna terblokir harus 403, got %d", code)
	}
	u := "ws" + strings.TrimPrefix(e.srv.URL, "http") + "/ws"
	hdr := http.Header{}
	base, _ := http.NewRequest("GET", e.srv.URL, nil)
	var cs []string
	for _, ck := range bad.http.Jar.Cookies(base.URL) {
		cs = append(cs, ck.Name+"="+ck.Value)
	}
	hdr.Set("Cookie", strings.Join(cs, "; "))
	if _, res, err := websocket.DefaultDialer.Dial(u, hdr); err == nil || res == nil || res.StatusCode != http.StatusForbidden {
		t.Fatalf("pengguna terblokir tidak boleh bisa membuka ws (res=%v err=%v)", res, err)
	}
}

func TestAdminAccessControl(t *testing.T) {
	e := newEnv(t)
	u := e.client(t)
	u.ready("user@example.com", "male", "ID")
	if code, _ := u.do("GET", "/api/admin/reports", ""); code != http.StatusForbidden {
		t.Fatalf("non-admin harus 403, got %d", code)
	}
	if code, _ := u.do("POST", "/api/admin/ban", `{"userId":"x"}`); code != http.StatusForbidden {
		t.Fatalf("non-admin tidak boleh memblokir, got %d", code)
	}
	adm := e.client(t)
	adm.ready("admin@example.com", "female", "ID")
	if code, body := adm.do("GET", "/api/admin/reports", ""); code != 200 || !strings.Contains(body, "reports") {
		t.Fatalf("admin harus bisa: %d %s", code, body)
	}
	if code, _ := adm.do("POST", "/api/admin/ban", `{"userId":"dev:user@example.com","reason":"uji","hours":1}`); code != 200 {
		t.Fatalf("admin ban gagal: %d", code)
	}
	if code, _ := u.do("GET", "/api/ice", ""); code != http.StatusForbidden {
		t.Fatalf("user yang diblokir admin harus 403, got %d", code)
	}
	if code, _ := adm.do("POST", "/api/admin/unban", `{"userId":"dev:user@example.com"}`); code != 200 {
		t.Fatal("unban gagal")
	}
	if code, _ := u.do("GET", "/api/ice", ""); code != 200 {
		t.Fatalf("setelah unban harus 200, got %d", code)
	}
}

func TestGoogleOAuthFlow(t *testing.T) {
	e := newEnv(t)
	c := e.client(t)
	c.http.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	res, err := c.http.Get(e.srv.URL + "/auth/google")
	if err != nil || res.StatusCode != http.StatusFound {
		t.Fatalf("start: %v %v", err, res)
	}
	loc := res.Header.Get("Location")
	state := loc[strings.Index(loc, "state=")+6:]

	// state tidak cocok -> ditolak
	if res, _ := c.http.Get(e.srv.URL + "/auth/google/callback?code=x&state=salah"); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("state salah harus 400, got %d", res.StatusCode)
	}
	// state benar -> sesi dibuat
	res, _ = c.http.Get(e.srv.URL + "/auth/google/callback?code=x&state=" + state)
	if res.StatusCode != http.StatusFound || res.Header.Get("Location") != "/" {
		t.Fatalf("callback: %d %s", res.StatusCode, res.Header.Get("Location"))
	}
	_, me := c.do("GET", "/api/me", "")
	if !strings.Contains(me, `"authenticated":true`) || !strings.Contains(me, "g@example.com") {
		t.Fatalf("harus login: %s", me)
	}
}

func TestStaticServing(t *testing.T) {
	e := newEnv(t)
	get := func(path, enc string) *http.Response {
		req, _ := http.NewRequest("GET", e.srv.URL+path, nil)
		req.Header.Set("Accept-Encoding", enc)
		tr := &http.Transport{DisableCompression: true}
		res, err := (&http.Client{Transport: tr}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	// SPA fallback + tidak di-cache
	res := get("/chat/room", "")
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || !strings.Contains(string(b), "SPA") || res.Header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("fallback SPA: %d %q %q", res.StatusCode, b, res.Header.Get("Cache-Control"))
	}
	// aset ber-hash: immutable + gzip bila diminta
	res = get("/assets/app-abc.js", "gzip")
	if res.Header.Get("Content-Encoding") != "gzip" || !strings.Contains(res.Header.Get("Cache-Control"), "immutable") ||
		!strings.Contains(res.Header.Get("Content-Type"), "javascript") {
		t.Fatalf("header aset: %v", res.Header)
	}
	zr, err := gzip.NewReader(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if body, _ := io.ReadAll(zr); string(body) != "console.log(1)" {
		t.Fatalf("isi gzip salah: %q", body)
	}
	// tanpa gzip -> versi asli
	res = get("/assets/app-abc.js", "")
	if b, _ := io.ReadAll(res.Body); res.Header.Get("Content-Encoding") != "" || string(b) != "console.log(1)" {
		t.Fatal("tanpa Accept-Encoding harus versi asli")
	}
	// berkas tidak ada dengan ekstensi -> 404 (bukan index.html)
	if res := get("/assets/hilang.js", ""); res.StatusCode != 404 {
		t.Fatalf("aset hilang harus 404, got %d", res.StatusCode)
	}
	// header keamanan
	if res := get("/", ""); res.Header.Get("X-Frame-Options") != "DENY" || !strings.Contains(res.Header.Get("Content-Security-Policy"), "default-src 'self'") {
		t.Fatal("header keamanan hilang")
	}
	// path traversal tidak boleh keluar dari akar
	if res := get("/../../etc/passwd", ""); res.StatusCode == 200 {
		if b, _ := io.ReadAll(res.Body); strings.Contains(string(b), "root:") {
			t.Fatal("path traversal berhasil!")
		}
	}
}

func TestDevLoginDisabledInProduction(t *testing.T) {
	e := newEnv(t)
	// bangun ulang dengan Dev=false
	dir := t.TempDir()
	users, _ := infra.NewUserStore(dir)
	repo, _ := infra.NewModerationStore(dir)
	signer, _ := infra.NewSigner([]byte("rahasia-uji-yang-cukup-panjang"))
	avatarStore, _ := infra.NewAvatarStore(dir)
	mod := usecase.NewModeration(repo, nil)
	lob := usecase.NewLobby(usecase.NewMatchmaker(10*time.Second, time.Second, nil), mod, nil)
	h := New(Deps{Cfg: Config{Dev: false}, Accounts: usecase.NewAccounts(users, nil, nil), Moderation: mod, Lobby: lob,
		Signer: signer, Identity: fakeIdentity{}, ICE: fakeICE{}, Avatar: avatarStore, Static: fstest.MapFS{"index.html": {Data: []byte("x")}}})
	srv := httptest.NewServer(h)
	defer srv.Close()
	res, _ := http.Get(srv.URL + "/auth/dev?email=x@example.com")
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("/auth/dev harus 404 di produksi, got %d", res.StatusCode)
	}
	_ = e
}

func (c *client) getWithXFF(path, xff string) int {
	c.t.Helper()
	req, _ := http.NewRequest("GET", c.e.srv.URL+path, nil)
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	res.Body.Close()
	return res.StatusCode
}

func TestRateLimitPublicIPIsStrictAndPerIP(t *testing.T) {
	e := newEnv(t)
	c := e.client(t)
	var last int
	for i := 0; i < 30; i++ {
		last = c.getWithXFF("/auth/dev", "8.8.8.8")
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("IP publik: setelah 20 percobaan/menit harus 429, got %d", last)
	}
	// IP publik lain tidak ikut terblokir
	if code := c.getWithXFF("/auth/dev", "1.1.1.1"); code == http.StatusTooManyRequests {
		t.Fatal("IP publik lain tidak boleh ikut terkena batas")
	}
}

// Server di belakang NAT: semua pengunjung tampak berasal dari IP privat. Satu pengunjung agresif
// tidak boleh langsung memblokir semua orang pada batas kecil per-IP.
func TestRateLimitPrivateIPUsesRelaxedSharedBucket(t *testing.T) {
	e := newEnv(t)
	c := e.client(t)
	for i := 0; i < 100; i++ { // jauh di atas batas 20/menit untuk IP publik
		if code := c.getWithXFF("/auth/dev", "10.10.0.1"); code == http.StatusTooManyRequests {
			t.Fatalf("permintaan ke-%d dari IP privat tidak boleh kena batas ketat", i+1)
		}
	}
	// tanpa XFF (loopback langsung) juga masuk bucket bersama yang longgar
	if code := c.getWithXFF("/auth/dev", ""); code == http.StatusTooManyRequests {
		t.Fatal("loopback tanpa XFF memakai bucket bersama yang longgar")
	}
	// tetap ada perlindungan banjir: bucket bersama punya batas
	var last int
	for i := 0; i < 20*sharedFactor+10; i++ {
		last = c.getWithXFF("/auth/dev", "10.10.0.1")
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("bucket bersama tetap harus punya batas, got %d", last)
	}
}

func TestClientKey(t *testing.T) {
	mk := func(remote, xff string) *http.Request {
		r, _ := http.NewRequest("GET", "/", nil)
		r.RemoteAddr = remote
		if xff != "" {
			r.Header.Set("X-Forwarded-For", xff)
		}
		return r
	}
	cases := []struct {
		name, remote, xff, key string
		factor                 int
	}{
		{"publik lewat Caddy", "127.0.0.1:5000", "203.0.113.9", "203.0.113.9", 1},
		{"NAT gateway lewat Caddy", "127.0.0.1:5000", "10.10.0.1", "shared", sharedFactor},
		{"loopback tanpa XFF", "127.0.0.1:5000", "", "shared", sharedFactor},
		{"koneksi langsung publik, XFF diabaikan", "198.51.100.7:4000", "1.2.3.4", "198.51.100.7", 1},
		{"koneksi langsung privat", "192.168.1.5:4000", "", "shared", sharedFactor},
		{"XFF sampah", "127.0.0.1:5000", "bukan-ip", "shared", sharedFactor},
	}
	for _, c := range cases {
		k, f := clientKey(mk(c.remote, c.xff))
		if k != c.key || f != c.factor {
			t.Errorf("%s: got (%q,%d) want (%q,%d)", c.name, k, f, c.key, c.factor)
		}
	}
}

// PNG kecil yang sah untuk uji unggah avatar.
func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 24, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 24; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 200, G: 20, B: 20, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestAvatarUploadAndReset(t *testing.T) {
	e := newEnv(t)
	c := e.client(t)
	c.ready("pic@example.com", "male", "ID")

	_, me := c.do("GET", "/api/me", "")
	if !strings.Contains(me, `"avatar":"/avatar/default.svg"`) {
		t.Fatalf("avatar awal harus default: %s", me)
	}

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, _ := w.CreateFormFile("avatar", "avatar.png")
	_, _ = fw.Write(tinyPNG(t))
	_ = w.Close()
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/profile/avatar", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	res, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("upload: %d %s", res.StatusCode, b)
	}
	var out map[string]string
	if err := json.Unmarshal(b, &out); err != nil || !strings.HasPrefix(out["avatar"], "/avatar/") {
		t.Fatalf("respons upload: %s", b)
	}

	_, me = c.do("GET", "/api/me", "")
	if !strings.Contains(me, out["avatar"]) {
		t.Fatalf("me harus memantulkan avatar: %s", me)
	}
	res, err = c.http.Get(e.srv.URL + out["avatar"])
	if err != nil || res.StatusCode != 200 || res.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("GET avatar: %v %v %v", err, res.StatusCode, res.Header.Get("Content-Type"))
	}
	pngB, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if img, _, err := image.Decode(bytes.NewReader(pngB)); err != nil || img.Bounds().Dx() != 128 {
		t.Fatalf("avatar harus PNG 128px: %v %v", img, err)
	}

	if res, _ := c.http.Get(e.srv.URL + "/avatar/..%2Fsecret"); res.StatusCode != 404 {
		t.Fatalf("path traversal harus 404, got %d", res.StatusCode)
	}

	res, _ = c.http.Post(e.srv.URL+"/api/profile/avatar/reset", "application/json", nil)
	b, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !strings.Contains(string(b), "/avatar/default.svg") {
		t.Fatalf("reset: %d %s", res.StatusCode, b)
	}

	var buf2 bytes.Buffer
	w2 := multipart.NewWriter(&buf2)
	f2, _ := w2.CreateFormFile("avatar", "x.txt")
	_, _ = f2.Write([]byte("bukan gambar"))
	_ = w2.Close()
	req2, _ := http.NewRequest("POST", e.srv.URL+"/api/profile/avatar", &buf2)
	req2.Header.Set("Content-Type", w2.FormDataContentType())
	res2, _ := c.http.Do(req2)
	if res2.StatusCode != http.StatusBadRequest {
		t.Fatalf("bukan gambar harus 400, got %d", res2.StatusCode)
	}
	res2.Body.Close()
}
