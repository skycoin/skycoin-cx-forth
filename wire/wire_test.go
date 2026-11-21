package wire

import (
	"bytes"
	"crypto/rand"
	mrand "math/rand/v2"
	"strings"
	"testing"
	"time"
)

var testKey = bytes.Repeat([]byte{7}, KeySize)

// loopConn hands client datagrams straight to a server and queues the
// replies. drop decides, per datagram in either direction, whether it is lost.
type loopConn struct {
	srv    *Server
	in     [][]byte
	n      int
	drop   func(n int) bool
	sent   [][]byte
	tamper func(b []byte)
}

func (c *loopConn) Send(b []byte) error {
	c.sent = append(c.sent, append([]byte{}, b...))
	c.n++
	if c.drop != nil && c.drop(c.n) {
		return nil
	}
	if c.tamper != nil {
		b = append([]byte{}, b...)
		c.tamper(b)
	}
	for _, r := range c.srv.Handle(b) {
		c.n++
		if c.drop != nil && c.drop(c.n) {
			continue
		}
		c.in = append(c.in, r)
	}
	return nil
}

func (c *loopConn) Recv(b []byte, _ time.Duration) (int, error) {
	if len(c.in) == 0 {
		return 0, nil
	}
	n := copy(b, c.in[0])
	c.in = c.in[1:]
	return n, nil
}

type echo struct{ calls int }

func (e *echo) handle(method, path, body string) (int, string) {
	e.calls++
	return 200, method + " " + path + " " + body
}

func pair(t *testing.T, h Handler) (*Client, *loopConn) {
	t.Helper()
	srv, err := NewServer(testKey, h, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	conn := &loopConn{srv: srv}
	c, err := NewClient(testKey, conn, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	c.Timeout = time.Millisecond
	return c, conn
}

func TestRoundTrip(t *testing.T) {
	e := &echo{}
	c, conn := pair(t, e.handle)
	for _, body := range []string{"", "hi", strings.Repeat("x", 10000)} {
		st, resp, err := c.Request("POST", "llm", body)
		if err != nil || st != 200 || resp != "POST llm "+body {
			t.Fatalf("got %d %.40q %v", st, resp, err)
		}
	}
	if e.calls != 3 {
		t.Fatalf("handler ran %d times", e.calls)
	}
	seen := map[string]bool{}
	for _, b := range conn.sent {
		n := string(b[3:headerSize])
		if seen[n] {
			t.Fatal("nonce reused")
		}
		seen[n] = true
	}
}

// Losing 30% of datagrams in both directions costs retries but never runs a
// request twice.
func TestLoss(t *testing.T) {
	e := &echo{}
	c, conn := pair(t, e.handle)
	rng := mrand.New(mrand.NewPCG(1, 2))
	conn.drop = func(int) bool { return rng.IntN(10) < 3 }
	c.Retries = 20
	body := strings.Repeat("y", 5000)
	for i := 0; i < 5; i++ {
		st, resp, err := c.Request("GET", "tg/getUpdates", body)
		if err != nil || st != 200 || resp != "GET tg/getUpdates "+body {
			t.Fatalf("request %d: %d %.40q %v", i, st, resp, err)
		}
	}
	if e.calls != 5 {
		t.Fatalf("handler ran %d times, want 5", e.calls)
	}
}

func TestSmallParts(t *testing.T) {
	e := &echo{}
	c, _ := pair(t, e.handle)
	c.Part = 48
	st, resp, err := c.Request("GET", "p", strings.Repeat("z", 3000))
	if err != nil || st != 200 || len(resp) != 3000+6 {
		t.Fatalf("got %d %d %v", st, len(resp), err)
	}
}

func TestRejects(t *testing.T) {
	e := &echo{}
	c, conn := pair(t, e.handle)
	conn.tamper = func(b []byte) { b[len(b)-1] ^= 1 }
	c.Retries = 1
	if _, _, err := c.Request("GET", "x", ""); err != ErrTimeout {
		t.Fatalf("tampered datagram answered: %v", err)
	}

	other, err := NewClient(bytes.Repeat([]byte{8}, KeySize), conn, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	conn.tamper = nil
	other.Timeout, other.Retries = time.Millisecond, 1
	if _, _, err := other.Request("GET", "x", ""); err != ErrTimeout {
		t.Fatalf("wrong key answered: %v", err)
	}
	if e.calls != 0 {
		t.Fatalf("handler ran %d times", e.calls)
	}

	// A replayed older request is ignored, the latest one gets the cache.
	conn.sent = nil
	if _, _, err := c.Request("GET", "one", ""); err != nil {
		t.Fatal(err)
	}
	first := conn.sent[0]
	if _, _, err := c.Request("GET", "two", ""); err != nil {
		t.Fatal(err)
	}
	if out := conn.srv.Handle(first); out != nil {
		t.Fatal("replayed request answered")
	}
	if e.calls != 2 {
		t.Fatalf("handler ran %d times, want 2", e.calls)
	}
}

func TestTooLarge(t *testing.T) {
	c, _ := pair(t, func(string, string, string) (int, string) {
		return 200, strings.Repeat("r", MaxParts*DefaultPart)
	})
	if _, _, err := c.Request("GET", "x", strings.Repeat("q", MaxMessage)); err == nil {
		t.Fatal("oversized request sent")
	}
	st, _, err := c.Request("GET", "x", "")
	if err != nil || st != 502 {
		t.Fatalf("oversized response: %d %v", st, err)
	}
}

func TestFields(t *testing.T) {
	f, err := Decode(Encode("a", "", strings.Repeat("b", 300)), 3)
	if err != nil || f[0] != "a" || f[1] != "" || len(f[2]) != 300 {
		t.Fatalf("got %q %v", f, err)
	}
	for _, b := range [][]byte{{5, 'a'}, Encode("a", "b"), {0x80}} {
		if _, err := Decode(b, 1); err == nil {
			t.Errorf("Decode(%q) accepted", b)
		}
	}
}

func FuzzDecode(f *testing.F) {
	f.Add(Encode("GET", "tg/x", "body"))
	f.Add([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01})
	f.Fuzz(func(t *testing.T, b []byte) {
		if fs, err := Decode(b, 3); err == nil && !bytes.Equal(Encode(fs...), b) {
			t.Fatalf("round trip changed %q", b)
		}
	})
}

// Arbitrary datagrams, including truncated and bit flipped real ones,
// never crash the server and never reach the handler.
func FuzzServer(f *testing.F) {
	s, _ := newSealer(testKey, bytes.NewReader(make([]byte, 16)))
	good := s.seal(&part{kindRequest, 1, 0, 1, 64, Encode("GET", "x", "")})
	f.Add(good, 0, byte(0))
	f.Add(good, len(good)-1, byte(1))
	f.Add([]byte("CX\x01"), 0, byte(0))
	f.Fuzz(func(t *testing.T, b []byte, at int, flip byte) {
		srv, _ := NewServer(testKey, func(string, string, string) (int, string) {
			t.Fatal("handler reached")
			return 0, ""
		}, rand.Reader)
		b = append([]byte{}, b...)
		if len(b) > 0 && flip != 0 {
			at = (at%len(b) + len(b)) % len(b)
			b[at] ^= flip
			if bytes.Equal(b, good) {
				return // the fuzzer undid its own flip
			}
			srv.Handle(b)
			return
		}
		if len(b) > 0 && !bytes.Equal(b, good) {
			srv.Handle(b[:len(b)/2])
		}
	})
}
