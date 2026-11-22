package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/skycoin/skycoin-cx-forth/agent"
	"github.com/skycoin/skycoin-cx-forth/cxir"
	"github.com/skycoin/skycoin-cx-forth/forth"
	"github.com/skycoin/skycoin-cx-forth/gateway"
	"github.com/skycoin/skycoin-cx-forth/host"
	"github.com/skycoin/skycoin-cx-forth/wire"
)

// agentMain runs the agent with the network on a UDP socket and GPIO in
// memory. Settings come from CXFORTH_* variables, see package agent.
func agentMain(server string, emit bool) error {
	if emit {
		p, err := agent.Program()
		if err != nil {
			return err
		}
		src, err := cxir.Translate(p)
		if err != nil {
			return err
		}
		fmt.Print(src)
		return nil
	}
	key, err := hex.DecodeString(os.Getenv("CXFORTH_KEY"))
	if err != nil || len(key) != wire.KeySize {
		return errors.New("set CXFORTH_KEY to the server's key, 64 hex digits")
	}
	addr, err := net.ResolveUDPAddr("udp", server)
	if err != nil {
		return err
	}
	uc, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return err
	}
	client, err := wire.NewClient(key, gateway.UDP{C: uc}, rand.Reader)
	if err != nil {
		return err
	}
	kv := map[string]string{}
	for k, env := range map[string]string{"agent.polls": "CXFORTH_POLLS", "llm.model": "CXFORTH_MODEL", "agent.system": "CXFORTH_SYSTEM"} {
		kv[k] = os.Getenv(env)
	}
	vm := forth.New(os.Stdout)
	err = agent.Load(vm, &host.Env{
		Net:   client,
		Pins:  host.MemPins{},
		KV:    kv,
		Now:   func() int64 { return time.Now().UnixMilli() },
		Sleep: func(ms int64) { time.Sleep(time.Duration(ms) * time.Millisecond) },
	})
	if err != nil {
		return err
	}
	return vm.Interpret(cxir.WordName("main"))
}
