package agent

import (
	"bytes"
	"crypto/rand"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/skycoin/skycoin-cx-forth/cxir"
	"github.com/skycoin/skycoin-cx-forth/forth"
	"github.com/skycoin/skycoin-cx-forth/gateway"
	"github.com/skycoin/skycoin-cx-forth/host"
	"github.com/skycoin/skycoin-cx-forth/internal/mock"
	"github.com/skycoin/skycoin-cx-forth/wire"
)

// The agent runs in the VM against mock Telegram and LLM endpoints behind
// a real gateway over UDP: messages in, tools called, replies out.
func TestAgent(t *testing.T) {
	tg := mock.NewTelegram("tok")
	defer tg.Close()
	tg.MaxWait = 300 * time.Millisecond
	llm := mock.NewLLM("key")
	defer llm.Close()

	key := bytes.Repeat([]byte{3}, wire.KeySize)
	cfg := &gateway.Config{TelegramAPI: tg.URL(), TelegramToken: tg.Token, LLMURL: llm.URL(), LLMKey: llm.Key}
	srv, err := wire.NewServer(key, cfg.Handle, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pc.Close() }()
	go func() { _ = gateway.Serve(pc, srv) }()
	uc, err := net.DialUDP("udp", nil, pc.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = uc.Close() }()
	client, err := wire.NewClient(key, gateway.UDP{C: uc}, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	pins := host.MemPins{}
	env := &host.Env{
		Net:   client,
		Pins:  pins,
		KV:    map[string]string{"agent.polls": "2", "tg.timeout": "1"},
		Now:   func() int64 { return 99 },
		Sleep: func(int64) {},
	}
	var out strings.Builder
	vm := forth.New(&out)
	if err := Load(vm, env); err != nil {
		t.Fatal(err)
	}

	says := []struct{ text, want string }{
		{"turn the led on", "done: pin 2 set to 1"},
		{"remember color blue", "done: stored"},
		{"recall color", "done: blue"},
		{"recall size", "done: nothing stored"},
		{"time", "done: 99"},
		{"hello \"there\"\nbye", "echo: hello \"there\"\nbye"},
	}
	for _, s := range says {
		tg.Say(7, s.text)
	}
	if err := vm.Interpret(cxir.WordName("main")); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	for _, s := range says {
		select {
		case r := <-tg.Sent():
			if r.ChatID != 7 || r.Text != s.want {
				t.Errorf("reply to %q: %+v, want %q", s.text, r, s.want)
			}
		default:
			t.Fatalf("no reply to %q\n%s", s.text, out.String())
		}
	}
	if pins[2] != 1 || env.KV["mem.color"] != "blue" {
		t.Errorf("pins %v kv %v", pins, env.KV)
	}
	reqs := llm.Requests()
	if len(reqs) != 11 {
		t.Errorf("%d LLM requests, want 11", len(reqs))
	}
	if r := string(reqs[1]); !strings.Contains(r, `"role":"tool","tool_call_id":"call_1","content":"pin 2 set to 1"`) ||
		!strings.Contains(r, `"name":"gpio_write"`) {
		t.Errorf("second request lacks the tool result or tools: %s", r)
	}
	t.Logf("console:\n%s", out.String())
	u := vm.Usage()
	t.Logf("code %d/%d instructions, %d words, %d literals, %d strings", u.Code, u.CodeMax, u.Words, u.Literals, u.Strings)
}

// The round trip per message on the desktop, mock services included.
func BenchmarkMessage(b *testing.B) {
	tg := mock.NewTelegram("tok")
	defer tg.Close()
	llm := mock.NewLLM("key")
	defer llm.Close()
	cfg := &gateway.Config{TelegramAPI: tg.URL(), TelegramToken: tg.Token, LLMURL: llm.URL(), LLMKey: llm.Key}
	key := bytes.Repeat([]byte{3}, wire.KeySize)
	srv, _ := wire.NewServer(key, cfg.Handle, rand.Reader)
	pc, _ := net.ListenPacket("udp", "127.0.0.1:0")
	defer func() { _ = pc.Close() }()
	go func() { _ = gateway.Serve(pc, srv) }()
	uc, _ := net.DialUDP("udp", nil, pc.LocalAddr().(*net.UDPAddr))
	client, _ := wire.NewClient(key, gateway.UDP{C: uc}, rand.Reader)
	env := &host.Env{Net: client, Pins: host.MemPins{}, Now: func() int64 { return 0 },
		Sleep: func(ms int64) { time.Sleep(time.Duration(ms) * time.Millisecond) }}
	vm := forth.New(nil)
	if err := Load(vm, env); err != nil {
		b.Fatal(err)
	}
	// The agent polls forever. It is left running when the benchmark ends.
	go func() { _ = vm.Interpret(cxir.WordName("main")) }()
	for b.Loop() {
		tg.Say(1, "turn the led on")
		<-tg.Sent()
	}
}
