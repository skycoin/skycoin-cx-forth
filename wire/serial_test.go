package wire

import (
	"bytes"
	"crypto/rand"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// ring is a board's UART receive buffer: 128 bytes, dropping on overflow,
// read slowly.
type ring struct {
	mu      sync.Mutex
	buf     []byte
	dropped int
}

func (r *ring) fill(src io.Reader) {
	b := make([]byte, 1)
	for {
		if _, err := src.Read(b); err != nil {
			return
		}
		r.mu.Lock()
		if len(r.buf) < 128 {
			r.buf = append(r.buf, b[0])
		} else {
			r.dropped++
		}
		r.mu.Unlock()
	}
}

func (r *ring) Buffered() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.buf)
}

func (r *ring) ReadByte() (byte, error) {
	time.Sleep(5 * time.Microsecond)
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.buf) == 0 {
		return 0, io.EOF
	}
	c := r.buf[0]
	r.buf = r.buf[1:]
	return c, nil
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

type duplex struct {
	io.Reader
	io.Writer
}

// A client on the board side of a serial line reaches a server through a
// relay. Console text passes through, and the ACK window keeps a slow
// 128 byte receive buffer from ever overflowing.
func TestSerialRelay(t *testing.T) {
	toBoard, fromRelay := io.Pipe()
	toRelay, fromBoard := io.Pipe()
	rb := &ring{}
	go rb.fill(toBoard)

	e := &echo{}
	srv, err := NewServer(testKey, e.handle, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	console := &lockedBuffer{}
	relay := NewRelay(duplex{toRelay, fromRelay}, console)
	frames := make(chan []byte, 64)
	go func() {
		_ = relay.Run(func(b []byte) { frames <- append([]byte{}, b...) })
	}()
	go func() {
		for f := range frames {
			for _, out := range srv.Handle(f) {
				if err := relay.Send(out); err != nil {
					t.Error(err)
				}
			}
		}
	}()

	board := &SerialConn{R: rb, W: fromBoard, Idle: time.Millisecond}
	c, err := NewClient(testKey, board, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	c.Part = 512
	_, _ = fromBoard.Write([]byte("boot ok\r\n"))
	for _, body := range []string{"", "x", strings.Repeat("b", 3000)} {
		st, resp, err := c.Request("POST", "llm", body)
		if err != nil || st != 200 || resp != "POST llm "+body {
			t.Fatalf("got %d %.40q %v", st, resp, err)
		}
		_, _ = fromBoard.Write([]byte("> done\r\n"))
	}
	time.Sleep(10 * time.Millisecond)
	if rb.dropped != 0 {
		t.Errorf("receive buffer dropped %d bytes", rb.dropped)
	}
	if e.calls != 3 {
		t.Errorf("handler ran %d times", e.calls)
	}
	if got := console.String(); got != "boot ok\r\n> done\r\n> done\r\n> done\r\n" {
		t.Errorf("console %q", got)
	}
}

// Noise around and inside frames never crashes the board side, and a
// frame after the noise still arrives.
func FuzzSerialConn(f *testing.F) {
	f.Add([]byte("\x01D00"), []byte("abc"))
	f.Add([]byte("\x01\x01Dzz\x01D"), []byte{})
	f.Fuzz(func(t *testing.T, noise, payload []byte) {
		if len(payload) > 300 {
			payload = payload[:300]
		}
		in := append(append([]byte{}, noise...), frameHeader(len(payload))...)
		in = append(in, payload...)
		r := &sliceReader{b: in}
		s := &SerialConn{R: r, W: io.Discard}
		buf := make([]byte, 512)
		for {
			n, err := s.Recv(buf, 0)
			if err != nil || n == 0 {
				return // the noise opened a frame that swallowed ours
			}
			if bytes.Equal(buf[:n], payload) && r.done() {
				return
			}
		}
	})
}

type sliceReader struct{ b []byte }

func (s *sliceReader) Buffered() int { return len(s.b) }
func (s *sliceReader) done() bool    { return len(s.b) == 0 }
func (s *sliceReader) ReadByte() (byte, error) {
	if len(s.b) == 0 {
		return 0, io.EOF
	}
	c := s.b[0]
	s.b = s.b[1:]
	return c, nil
}
