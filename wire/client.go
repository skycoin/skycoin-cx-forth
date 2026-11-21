package wire

import (
	"errors"
	"io"
	"strconv"
	"time"
)

// Conn sends and receives whole datagrams. A UDP socket is one, and so is
// a serial link that frames datagrams for a relay.
type Conn interface {
	Send(b []byte) error
	// Recv waits up to timeout for one datagram. It returns n == 0 and a
	// nil error on timeout.
	Recv(b []byte, timeout time.Duration) (int, error)
}

// ErrTimeout is returned when no response arrives after every retry.
var ErrTimeout = errors.New("wire: no response")

// Client makes requests to a gateway.
type Client struct {
	s    *sealer
	conn Conn
	id   uint32
	buf  []byte
	resp assembly

	// Part is the largest payload per datagram in both directions.
	Part int
	// Timeout is the first wait for a response. It doubles on every retry.
	Timeout time.Duration
	// Retries is how many times a request is sent again.
	Retries int
}

// NewClient returns a client using key, which must be KeySize bytes, and
// rand to pick its nonce prefix.
func NewClient(key []byte, conn Conn, rand io.Reader) (*Client, error) {
	s, err := newSealer(key, rand)
	if err != nil {
		return nil, err
	}
	return &Client{s: s, conn: conn, Part: DefaultPart, Timeout: time.Second, Retries: 6}, nil
}

// Request sends a request and returns the gateway's status and body.
func (c *Client) Request(method, path, body string) (int, string, error) {
	c.id++
	msg := Encode(method, path, body)
	if len(msg) > MaxMessage || (len(msg)+c.Part-1)/c.Part > MaxParts {
		return 0, "", errors.New("wire: request too large")
	}
	parts := c.s.split(kindRequest, c.id, uint16(c.Part), msg, c.Part)
	if len(c.buf) < c.Part+Overhead {
		c.buf = make([]byte, c.Part+Overhead)
	}
	c.resp = assembly{}
	wait := c.Timeout
	for try := 0; try <= c.Retries; try++ {
		for _, p := range parts {
			if err := c.conn.Send(p); err != nil {
				return 0, "", err
			}
		}
		if b, ok, err := c.collect(wait); err != nil || ok {
			if err != nil {
				return 0, "", err
			}
			f, err := Decode(b, 2)
			if err != nil {
				return 0, "", err
			}
			st, err := strconv.Atoi(f[0])
			if err != nil {
				return 0, "", errors.New("wire: bad status")
			}
			return st, f[1], nil
		}
		wait *= 2
	}
	return 0, "", ErrTimeout
}

// collect reads response parts for the current id until the message is
// whole or wait passes without one arriving.
func (c *Client) collect(wait time.Duration) ([]byte, bool, error) {
	deadline := time.Now().Add(wait)
	for {
		left := time.Until(deadline)
		if left <= 0 {
			return nil, false, nil
		}
		n, err := c.conn.Recv(c.buf, left)
		if err != nil {
			return nil, false, err
		}
		if n == 0 {
			return nil, false, nil
		}
		_, p, err := open(c.s.aead, c.buf[:n])
		if err != nil || p.kind != kindResponse || p.id != c.id {
			continue
		}
		done, err := c.resp.add(&p)
		if err != nil {
			return nil, false, err
		}
		if done {
			return c.resp.bytes(), true, nil
		}
	}
}
