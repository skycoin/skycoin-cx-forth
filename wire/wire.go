// Package wire is the encrypted request and response protocol between a
// device and its gateway, carried in UDP datagrams.
//
// A request is a method, a path and a body. The response is a status and a
// body. The gateway decides what a path means, so the device holds no URLs,
// tokens or API keys, only the pre-shared key.
//
// # Datagram
//
//	"CX" version(1) nonce(24) sealed
//
// sealed is XChaCha20-Poly1305 over the header below with the first 27
// bytes as additional data. XChaCha's 24 byte nonce lets every sender pick
// a random 16 byte prefix when it starts and count in the last 8 bytes, so
// a reboot never reuses a nonce under the same key.
//
//	kind(1) id(4) part(2) parts(2) max(2) payload
//
// kind is 'Q' for a request part and 'R' for a response part. id numbers
// the client's requests from 1 and a response carries the id it answers.
// part and parts split one message into up to MaxParts datagrams. max, in a
// request, is the largest response payload per datagram the client wants.
// All integers are big endian.
//
// A message is a list of fields, each a uvarint length then the bytes. A
// request is method, path, body. A response is the status in decimal, then
// the body.
//
// # Loss and replay
//
// The client sends every part of a request and waits for the whole
// response, sending the request again after a timeout that doubles each
// time. The server keeps the last response of each client session, keyed by
// the client's nonce prefix, and answers a repeated id from that cache
// instead of running the request again. It ignores ids lower than the last.
package wire

import (
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"io"

	"golang.org/x/crypto/chacha20poly1305"
)

// Sizes and limits.
const (
	KeySize    = chacha20poly1305.KeySize
	Version    = 1
	headerSize = 2 + 1 + chacha20poly1305.NonceSizeX
	innerSize  = 1 + 4 + 2 + 2 + 2
	Overhead   = headerSize + innerSize + chacha20poly1305.Overhead
	MaxParts   = 256
	MaxMessage = 64 << 10
	// DefaultPart keeps a datagram under a 1280 byte IPv6 minimum MTU.
	DefaultPart = 1024
)

const (
	kindRequest  = 'Q'
	kindResponse = 'R'
)

var errBad = errors.New("wire: bad datagram")

// sealer seals datagrams under one key with a random nonce prefix.
type sealer struct {
	aead   cipher.AEAD
	prefix [16]byte
	ctr    uint64
}

func newSealer(key []byte, rand io.Reader) (*sealer, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	s := &sealer{aead: aead}
	if _, err := io.ReadFull(rand, s.prefix[:]); err != nil {
		return nil, err
	}
	return s, nil
}

type part struct {
	kind        byte
	id          uint32
	part, parts uint16
	max         uint16
	payload     []byte
}

func (s *sealer) seal(p *part) []byte {
	s.ctr++
	b := make([]byte, headerSize, headerSize+innerSize+len(p.payload)+chacha20poly1305.Overhead)
	b[0], b[1], b[2] = 'C', 'X', Version
	copy(b[3:], s.prefix[:])
	binary.BigEndian.PutUint64(b[3+16:], s.ctr)
	in := make([]byte, innerSize, innerSize+len(p.payload))
	in[0] = p.kind
	binary.BigEndian.PutUint32(in[1:], p.id)
	binary.BigEndian.PutUint16(in[5:], p.part)
	binary.BigEndian.PutUint16(in[7:], p.parts)
	binary.BigEndian.PutUint16(in[9:], p.max)
	in = append(in, p.payload...)
	return s.aead.Seal(b, b[3:headerSize], in, b[:headerSize])
}

// open checks and decrypts a datagram. It returns the sender's nonce
// prefix along with the part.
func open(aead cipher.AEAD, b []byte) (prefix [16]byte, p part, err error) {
	if len(b) < headerSize+innerSize+chacha20poly1305.Overhead || b[0] != 'C' || b[1] != 'X' || b[2] != Version {
		return prefix, p, errBad
	}
	in, err := aead.Open(nil, b[3:headerSize], b[headerSize:], b[:headerSize])
	if err != nil {
		return prefix, p, errBad
	}
	copy(prefix[:], b[3:])
	p = part{
		kind:    in[0],
		id:      binary.BigEndian.Uint32(in[1:]),
		part:    binary.BigEndian.Uint16(in[5:]),
		parts:   binary.BigEndian.Uint16(in[7:]),
		max:     binary.BigEndian.Uint16(in[9:]),
		payload: in[innerSize:],
	}
	if p.parts == 0 || p.parts > MaxParts || p.part >= p.parts {
		return prefix, p, errBad
	}
	return prefix, p, nil
}

// split seals msg as parts of at most size payload bytes.
func (s *sealer) split(kind byte, id uint32, max uint16, msg []byte, size int) [][]byte {
	n := (len(msg) + size - 1) / size
	if n == 0 {
		n = 1
	}
	out := make([][]byte, 0, n)
	for i := 0; i < n; i++ {
		end := min((i+1)*size, len(msg))
		out = append(out, s.seal(&part{kind, id, uint16(i), uint16(n), max, msg[i*size : end]}))
	}
	return out
}

// assembly collects the parts of one message.
type assembly struct {
	id    uint32
	parts [][]byte
	have  int
	size  int
}

// add stores p and reports whether the message is complete.
func (a *assembly) add(p *part) (bool, error) {
	if a.parts == nil || a.id != p.id || len(a.parts) != int(p.parts) {
		a.id, a.parts, a.have, a.size = p.id, make([][]byte, p.parts), 0, 0
	}
	if a.parts[p.part] == nil {
		if a.size+len(p.payload) > MaxMessage {
			a.parts = nil
			return false, errors.New("wire: message too large")
		}
		a.parts[p.part] = append([]byte{}, p.payload...)
		a.have++
		a.size += len(p.payload)
	}
	return a.have == len(a.parts), nil
}

func (a *assembly) bytes() []byte {
	b := make([]byte, 0, a.size)
	for _, p := range a.parts {
		b = append(b, p...)
	}
	return b
}

// Encode joins fields into a message.
func Encode(fields ...string) []byte {
	n := 0
	for _, f := range fields {
		n += binary.MaxVarintLen64 + len(f)
	}
	b := make([]byte, 0, n)
	for _, f := range fields {
		b = binary.AppendUvarint(b, uint64(len(f)))
		b = append(b, f...)
	}
	return b
}

// Decode splits a message into exactly n fields.
func Decode(b []byte, n int) ([]string, error) {
	out := make([]string, 0, n)
	for len(b) > 0 {
		l, k := binary.Uvarint(b)
		if k <= 0 || l > uint64(len(b)-k) || len(out) == n {
			return nil, errors.New("wire: bad message")
		}
		out = append(out, string(b[k:k+int(l)]))
		b = b[k+int(l):]
	}
	if len(out) != n {
		return nil, errors.New("wire: bad message")
	}
	return out, nil
}
