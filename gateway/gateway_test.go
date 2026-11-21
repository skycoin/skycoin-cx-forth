package gateway

import (
	"bytes"
	"crypto/rand"
	"net"
	"strings"
	"testing"

	"github.com/skycoin/skycoin-cx-forth/internal/mock"
	"github.com/skycoin/skycoin-cx-forth/wire"
)

func TestGateway(t *testing.T) {
	tg := mock.NewTelegram("123:abc")
	defer tg.Close()
	llm := mock.NewLLM("sk-test")
	defer llm.Close()
	key := bytes.Repeat([]byte{1}, wire.KeySize)
	cfg := &Config{TelegramAPI: tg.URL(), TelegramToken: "123:abc", LLMURL: llm.URL(), LLMKey: "sk-test"}
	srv, err := wire.NewServer(key, cfg.Handle, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pc.Close() }()
	go func() { _ = Serve(pc, srv) }()

	uc, err := net.DialUDP("udp", nil, pc.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = uc.Close() }()
	c, err := wire.NewClient(key, UDP{uc}, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tg.Say(42, "hello")
	cases := []struct {
		method, path, body string
		status             int
		want               string
	}{
		{"GET", "tg/getUpdates?timeout=0", "", 200, `"text":"hello"`},
		{"POST", "tg/sendMessage", `{"chat_id":42,"text":"hi"}`, 200, `"ok":true`},
		{"POST", "llm", `{"model":"m","messages":[{"role":"user","content":"x"}]}`, 200, `echo: x`},
		{"GET", "tg/getMe", "", 404, `Not Found`},
		{"GET", "elsewhere", "", 404, `no route`},
		{"DELETE", "llm", "", 405, `not allowed`},
	}
	for _, k := range cases {
		st, body, err := c.Request(k.method, k.path, k.body)
		if err != nil || st != k.status || !strings.Contains(body, k.want) {
			t.Errorf("%s %s: %d %q %v", k.method, k.path, st, body, err)
		}
	}
	if s := <-tg.Sent(); s.ChatID != 42 || s.Text != "hi" {
		t.Errorf("sent %+v", s)
	}

	// A wrong token stays inside the gateway: the device sees only the status.
	bad := &Config{TelegramAPI: tg.URL(), TelegramToken: "wrong"}
	if st, body := bad.Handle("GET", "tg/getUpdates", ""); st != 401 || strings.Contains(body, "wrong") {
		t.Errorf("wrong token: %d %q", st, body)
	}
}
