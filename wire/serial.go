package wire

import (
	"errors"
	"io"
	"time"
)

// Serial framing carries datagrams over a UART shared with console text,
// for a board with no network of its own. A relay on the other end of the
// line forwards each datagram as UDP, so the protocol is the same end to end.
//
//	SOH 'D' len(4 hex digits) datagram
//
// Bytes outside a frame are console text. A board's receive buffer can be
// as small as 128 bytes, so the relay sends at most Window bytes of a frame,
// counting from SOH, and waits for an ACK byte before the next Window. The
// board sends frames without waiting.
const (
	SOH    = 0x01
	ACK    = 0x06
	Window = 64
	// MaxFrame bounds a datagram on a serial line.
	MaxFrame = 0xffff
)

func frameHeader(n int) []byte {
	const hex = "0123456789abcdef"
	return []byte{SOH, 'D', hex[n>>12&15], hex[n>>8&15], hex[n>>4&15], hex[n&15]}
}

func unhex(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

// ByteReader is the receive side of a UART: it never blocks.
type ByteReader interface {
	Buffered() int
	ReadByte() (byte, error)
}

// SerialConn is the board's end of a serial line, a Conn.
type SerialConn struct {
	R ByteReader
	W io.Writer
	// Idle is the pause while waiting for input.
	Idle time.Duration
}

// Send writes one framed datagram.
func (s *SerialConn) Send(b []byte) error {
	if len(b) > MaxFrame {
		return errors.New("wire: datagram too large for a serial frame")
	}
	if _, err := s.W.Write(frameHeader(len(b))); err != nil {
		return err
	}
	_, err := s.W.Write(b)
	return err
}

// Recv reads one framed datagram, skipping anything else, and acknowledges
// every Window bytes of it.
func (s *SerialConn) Recv(b []byte, timeout time.Duration) (int, error) {
	deadline := time.Now().Add(timeout)
	var hdr [6]byte
	nh, size, n := 0, 0, 0
	for {
		if s.R.Buffered() == 0 {
			if time.Now().After(deadline) {
				return 0, nil
			}
			time.Sleep(s.Idle)
			continue
		}
		c, err := s.R.ReadByte()
		if err != nil {
			continue
		}
		if nh < len(hdr) {
			switch {
			case nh == 0 && c != SOH, nh == 1 && c != 'D':
				nh = 0
				continue
			case nh >= 2 && unhex(c) < 0:
				nh = 0
				continue
			}
			hdr[nh] = c
			nh++
			if nh == len(hdr) {
				size = unhex(hdr[2])<<12 | unhex(hdr[3])<<8 | unhex(hdr[4])<<4 | unhex(hdr[5])
			}
		} else {
			if n < len(b) {
				b[n] = c
			}
			n++
		}
		if got := nh + n; got%Window == 0 {
			if _, err := s.W.Write([]byte{ACK}); err != nil {
				return 0, err
			}
		}
		if nh == len(hdr) && n == size {
			if n > len(b) {
				return 0, errors.New("wire: frame larger than buffer")
			}
			return n, nil
		}
	}
}

// Relay is the host's end of a serial line.
type Relay struct {
	line    io.ReadWriter
	console io.Writer
	acks    chan struct{}
	// AckTimeout is how long Send waits for each ACK.
	AckTimeout time.Duration
}

// NewRelay returns a relay on line that copies console text to console.
func NewRelay(line io.ReadWriter, console io.Writer) *Relay {
	return &Relay{line: line, console: console, acks: make(chan struct{}, 4), AckTimeout: 2 * time.Second}
}

// Run reads the line until it fails, calling frame for every datagram the
// board sends and copying everything else to Console.
func (r *Relay) Run(frame func([]byte)) error {
	buf := make([]byte, 4096)
	var hdr []byte
	var data []byte
	size := -1
	for {
		n, err := r.line.Read(buf)
		var text []byte
		for _, c := range buf[:n] {
			switch {
			case size >= 0:
				data = append(data, c)
				if len(data) == size {
					frame(data)
					data, size = nil, -1
				}
			case len(hdr) > 0:
				hdr = append(hdr, c)
				if (len(hdr) == 2 && c != 'D') || (len(hdr) > 2 && unhex(c) < 0) {
					text = append(text, hdr...)
					hdr = nil
				} else if len(hdr) == 6 {
					size = unhex(hdr[2])<<12 | unhex(hdr[3])<<8 | unhex(hdr[4])<<4 | unhex(hdr[5])
					hdr = nil
					if size == 0 {
						frame(nil)
						size = -1
					}
				}
			case c == SOH:
				hdr = append(hdr[:0], c)
			case c == ACK:
				select {
				case r.acks <- struct{}{}:
				default:
				}
			default:
				text = append(text, c)
			}
		}
		if len(text) > 0 && r.console != nil {
			_, _ = r.console.Write(text)
		}
		if err != nil {
			return err
		}
	}
}

// Send writes one framed datagram to the board, a Window at a time. Call
// Run first, since it delivers the ACKs. Only one Send may run at a time.
func (r *Relay) Send(b []byte) error {
	if len(b) > MaxFrame {
		return errors.New("wire: datagram too large for a serial frame")
	}
	for len(r.acks) > 0 {
		<-r.acks
	}
	f := append(frameHeader(len(b)), b...)
	for len(f) > 0 {
		k := min(Window, len(f))
		if _, err := r.line.Write(f[:k]); err != nil {
			return err
		}
		f = f[k:]
		if k < Window {
			break
		}
		select {
		case <-r.acks:
		case <-time.After(r.AckTimeout):
			return errors.New("wire: no ACK from the board")
		}
	}
	return nil
}
