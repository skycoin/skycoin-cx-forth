// Command cxforth-server is the gateway a cxforth agent talks to over
// encrypted UDP (package wire). It holds the Telegram bot token and the
// LLM API key, so the device holds only the pre-shared key.
//
//	cxforth-server -genkey                 print a new key
//	CXFORTH_KEY=<hex> TG_TOKEN=... LLM_URL=https://.../v1/chat/completions LLM_KEY=... cxforth-server
//	CXFORTH_KEY=<hex> cxforth-server -mock -say 'turn the led on' -say 'hi'
//
// With -mock it serves fake Telegram and LLM endpoints instead, sends each
// -say message after the reply to the one before, and prints the time from
// message to reply.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/skycoin/skycoin-cx-forth/gateway"
	"github.com/skycoin/skycoin-cx-forth/internal/mock"
	"github.com/skycoin/skycoin-cx-forth/wire"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:7400", "UDP address to listen on")
	genkey := flag.Bool("genkey", false, "print a new random key and exit")
	useMock := flag.Bool("mock", false, "serve fake Telegram and LLM endpoints")
	var says []string
	flag.Func("say", "with -mock, a user message to send (repeatable)", func(s string) error {
		says = append(says, s)
		return nil
	})
	wait := flag.Duration("wait", 2*time.Minute, "with -say, how long to wait for each reply")
	flag.Parse()

	if *genkey {
		k := make([]byte, wire.KeySize)
		if _, err := rand.Read(k); err != nil {
			fatal(err)
		}
		fmt.Println(hex.EncodeToString(k))
		return
	}
	key, err := hex.DecodeString(os.Getenv("CXFORTH_KEY"))
	if err != nil || len(key) != wire.KeySize {
		fatal(errors.New("set CXFORTH_KEY to 64 hex digits, see -genkey"))
	}

	cfg := &gateway.Config{
		TelegramAPI:   os.Getenv("TG_API"),
		TelegramToken: os.Getenv("TG_TOKEN"),
		LLMURL:        os.Getenv("LLM_URL"),
		LLMKey:        os.Getenv("LLM_KEY"),
	}
	if cfg.TelegramAPI == "" {
		cfg.TelegramAPI = "https://api.telegram.org"
	}
	var tg *mock.Telegram
	if *useMock {
		tg = mock.NewTelegram("mock-token")
		tg.MaxWait = 25 * time.Second
		llm := mock.NewLLM("mock-key")
		cfg = &gateway.Config{TelegramAPI: tg.URL(), TelegramToken: tg.Token, LLMURL: llm.URL(), LLMKey: llm.Key}
	}

	srv, err := wire.NewServer(key, cfg.Handle, rand.Reader)
	if err != nil {
		fatal(err)
	}
	pc, err := net.ListenPacket("udp", *listen)
	if err != nil {
		fatal(err)
	}
	fmt.Fprintln(os.Stderr, "cxforth-server: listening on", pc.LocalAddr())
	if tg == nil || len(says) == 0 {
		fatal(gateway.Serve(pc, srv))
	}
	go func() { fatal(gateway.Serve(pc, srv)) }()

	var total time.Duration
	for _, s := range says {
		at := tg.Say(42, s)
		select {
		case r := <-tg.Sent():
			d := r.At.Sub(at)
			total += d
			fmt.Printf("%q -> %q in %v\n", s, r.Text, d.Round(time.Millisecond))
		case <-time.After(*wait):
			fatal(fmt.Errorf("no reply to %q within %v", s, *wait))
		}
	}
	fmt.Printf("%d messages, mean round trip %v\n", len(says), (total / time.Duration(len(says))).Round(time.Millisecond))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "cxforth-server:", err)
	os.Exit(1)
}
