package wire

import (
	"io"
	"strconv"
)

// Handler answers one request.
type Handler func(method, path, body string) (status int, resp string)

// MaxSessions bounds the client sessions a server remembers.
const MaxSessions = 64

type session struct {
	req    assembly
	last   uint32
	cached [][]byte
	used   uint64
}

// Server answers requests. It does no I/O itself: feed it datagrams with
// Handle and send back what it returns.
type Server struct {
	s        *sealer
	h        Handler
	sessions map[[16]byte]*session
	tick     uint64
}

// NewServer returns a server using key and rand to pick its nonce prefix.
func NewServer(key []byte, h Handler, rand io.Reader) (*Server, error) {
	s, err := newSealer(key, rand)
	if err != nil {
		return nil, err
	}
	return &Server{s: s, h: h, sessions: map[[16]byte]*session{}}, nil
}

// Handle takes one datagram and returns the datagrams to send back, if any.
// It runs the handler when a request completes.
func (srv *Server) Handle(b []byte) [][]byte {
	prefix, p, err := open(srv.s.aead, b)
	if err != nil || p.kind != kindRequest || p.id == 0 {
		return nil
	}
	ss := srv.session(prefix)
	switch {
	case p.id < ss.last:
		return nil
	case p.id == ss.last:
		if p.part == p.parts-1 {
			return ss.cached
		}
		return nil
	}
	done, err := ss.req.add(&p)
	if err != nil || !done {
		return nil
	}
	f, err := Decode(ss.req.bytes(), 3)
	ss.req = assembly{}
	status, resp := 400, "bad request"
	if err == nil {
		status, resp = srv.h(f[0], f[1], f[2])
	}
	size := int(p.max)
	if size <= 0 || size > DefaultPart {
		size = DefaultPart
	}
	msg := Encode(strconv.Itoa(status), resp)
	if (len(msg)+size-1)/size > MaxParts {
		msg = Encode("502", "response too large")
	}
	ss.last = p.id
	ss.cached = srv.s.split(kindResponse, p.id, 0, msg, size)
	return ss.cached
}

func (srv *Server) session(prefix [16]byte) *session {
	srv.tick++
	ss := srv.sessions[prefix]
	if ss == nil {
		if len(srv.sessions) >= MaxSessions {
			var old [16]byte
			oldest := srv.tick
			for k, v := range srv.sessions {
				if v.used < oldest {
					old, oldest = k, v.used
				}
			}
			delete(srv.sessions, old)
		}
		ss = &session{}
		srv.sessions[prefix] = ss
	}
	ss.used = srv.tick
	return ss
}
