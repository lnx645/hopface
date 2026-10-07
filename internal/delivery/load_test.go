package delivery

import (
	"fmt"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"hopface/internal/domain"
	"hopface/internal/usecase"
)

// TestLoad hanya jalan bila HOPFACE_LOAD=N (jumlah klien, genap). Contoh:
//
//	HOPFACE_LOAD=400 go test -run TestLoad -v ./internal/delivery/
//
// Mengukur waktu sampai terhubung, latensi chat, dan memori server+klien (satu proses).
func TestLoad(t *testing.T) {
	n, _ := strconv.Atoi(os.Getenv("HOPFACE_LOAD"))
	if n < 2 {
		t.Skip("set HOPFACE_LOAD=N untuk menjalankan uji beban")
	}
	n -= n % 2
	e := newEnv(t)
	base := strings.TrimPrefix(e.srv.URL, "http")

	heap := func() (uint64, uint64) {
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return m.HeapAlloc >> 20, m.Sys >> 20
	}
	h0, s0 := heap()

	type cl struct {
		ws *websocket.Conn
		id string
	}
	cls := make([]*cl, n)
	for i := range cls {
		id := fmt.Sprintf("load%d@example.com", i)
		u, err := e.accounts.Upsert("dev:"+id, id, fmt.Sprintf("U%d", i), "")
		if err != nil {
			t.Fatal(err)
		}
		g := domain.GenderMale
		if i%2 == 1 {
			g = domain.GenderFemale
		}
		if _, err := e.accounts.UpdateProfile(u.ID, u.Name, "1995-03-04", g, "ID"); err != nil {
			t.Fatal(err)
		}
		hdr := http.Header{}
		hdr.Set("Cookie", cookieSession+"="+e.signer.Issue(u.ID, time.Hour))
		ws, _, err := websocket.DefaultDialer.Dial("ws"+base+"/ws", hdr)
		if err != nil {
			t.Fatalf("dial %d: %v", i, err)
		}
		cls[i] = &cl{ws: ws, id: u.ID}
	}
	defer func() {
		for _, c := range cls {
			c.ws.Close()
		}
	}()
	t.Logf("%d koneksi WebSocket terbuka", n)

	// semua mulai mencari bersamaan; ukur sampai tiap klien menerima "matched"
	var wg sync.WaitGroup
	var mu sync.Mutex
	var matchLat []time.Duration
	partner := make([]int, n) // indeks pasangan sebenarnya, dibaca dari event matched
	t0 := time.Now()
	for idx, c := range cls {
		wg.Add(1)
		go func(idx int, c *cl) {
			defer wg.Done()
			_ = c.ws.WriteJSON(map[string]string{"t": "start"})
			_ = c.ws.SetReadDeadline(time.Now().Add(15 * time.Second))
			for {
				var ev usecase.Event
				if err := c.ws.ReadJSON(&ev); err != nil {
					t.Errorf("tunggu matched: %v", err)
					return
				}
				if ev.T == "matched" {
					pi, _ := strconv.Atoi(strings.TrimPrefix(ev.Peer.Name, "U"))
					mu.Lock()
					partner[idx] = pi
					matchLat = append(matchLat, time.Since(t0))
					mu.Unlock()
					return
				}
			}
		}(idx, c)
	}
	wg.Wait()
	if len(matchLat) != n {
		t.Fatalf("hanya %d dari %d yang terhubung", len(matchLat), n)
	}
	sort.Slice(matchLat, func(i, j int) bool { return matchLat[i] < matchLat[j] })
	t.Logf("matched: %d klien, p50=%v p95=%v max=%v", n, matchLat[n/2], matchLat[n*95/100], matchLat[n-1])

	// latensi chat antar pasangan sebenarnya (3 putaran, tiap pasangan satu pesan per putaran)
	for i := range partner {
		if partner[partner[i]] != i {
			t.Fatalf("pasangan tidak simetris: %d -> %d -> %d", i, partner[i], partner[partner[i]])
		}
	}
	var chatLat []time.Duration
	for round := 0; round < 3; round++ {
		var wg2 sync.WaitGroup
		for i := 0; i < n; i++ {
			if partner[i] < i {
				continue // tiap pasangan dihitung sekali
			}
			wg2.Add(1)
			go func(a, b *cl) {
				defer wg2.Done()
				s := time.Now()
				_ = a.ws.WriteJSON(map[string]string{"t": "chat", "text": "ping"})
				_ = b.ws.SetReadDeadline(time.Now().Add(5 * time.Second))
				for {
					var ev usecase.Event
					if err := b.ws.ReadJSON(&ev); err != nil {
						t.Errorf("chat: %v", err)
						return
					}
					if ev.T == "chat" {
						mu.Lock()
						chatLat = append(chatLat, time.Since(s))
						mu.Unlock()
						return
					}
				}
			}(cls[i], cls[partner[i]])
		}
		wg2.Wait()
	}
	sort.Slice(chatLat, func(i, j int) bool { return chatLat[i] < chatLat[j] })
	m := len(chatLat)
	t.Logf("chat: %d pesan, p50=%v p95=%v max=%v", m, chatLat[m/2], chatLat[m*95/100], chatLat[m-1])

	h1, s1 := heap()
	t.Logf("memori (server+klien, satu proses): heap %dMB -> %dMB, total dari OS %dMB -> %dMB, goroutine=%d",
		h0, h1, s0, s1, runtime.NumGoroutine())
	on, _ := e.lob.Stats()
	if on != n {
		t.Fatalf("online=%d, harusnya %d", on, n)
	}
}
