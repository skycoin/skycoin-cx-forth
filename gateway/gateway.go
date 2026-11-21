// Package gateway is the server side of package wire. It turns a device's
// requests into calls to the Telegram Bot API and an OpenAI style chat
// completions endpoint, adding the credentials the device never holds.
//
//	tg/<method>[?query]   <TelegramAPI>/bot<token>/<method>[?query]
//	llm                   <LLMURL> with Authorization: Bearer <key>
package gateway

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	"github.com/skycoin/skycoin-cx-forth/wire"
)

// Config names the upstream services and their credentials.
type Config struct {
	TelegramAPI   string // for example https://api.telegram.org
	TelegramToken string
	LLMURL        string // the full chat completions URL
	LLMKey        string
	Client        *http.Client
}

// Handle is a wire.Handler.
func (c *Config) Handle(method, path, body string) (int, string) {
	var url, auth string
	switch {
	case strings.HasPrefix(path, "tg/") && c.TelegramAPI != "":
		url = c.TelegramAPI + "/bot" + c.TelegramToken + "/" + path[3:]
	case path == "llm" && c.LLMURL != "":
		url = c.LLMURL
		if c.LLMKey != "" {
			auth = "Bearer " + c.LLMKey
		}
	default:
		return 404, "no route for " + path
	}
	if method != "GET" && method != "POST" {
		return 405, "method not allowed"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader([]byte(body)))
	if err != nil {
		return 400, err.Error()
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	hc := c.Client
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		// The error text can hold the URL, and with it the bot token.
		return 502, "upstream request failed"
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(resp.Body, wire.MaxMessage-64))
	if err != nil {
		return 502, "upstream read failed"
	}
	return resp.StatusCode, string(b)
}

// Serve answers datagrams on pc until it is closed.
func Serve(pc net.PacketConn, srv *wire.Server) error {
	buf := make([]byte, 64<<10)
	for {
		n, addr, err := pc.ReadFrom(buf)
		if err != nil {
			return err
		}
		for _, out := range srv.Handle(buf[:n]) {
			if _, err := pc.WriteTo(out, addr); err != nil {
				break
			}
		}
	}
}

// UDP is a wire.Conn over a connected UDP socket.
type UDP struct{ C *net.UDPConn }

// Send writes one datagram.
func (u UDP) Send(b []byte) error {
	_, err := u.C.Write(b)
	return err
}

// Recv reads one datagram or times out.
func (u UDP) Recv(b []byte, timeout time.Duration) (int, error) {
	if err := u.C.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return 0, err
	}
	n, err := u.C.Read(b)
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		return 0, nil
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		// Nothing listens yet. Wait it out so the client retries later.
		time.Sleep(timeout)
		return 0, nil
	}
	return n, err
}
