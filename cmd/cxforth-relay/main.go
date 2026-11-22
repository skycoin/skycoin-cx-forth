// Command cxforth-relay connects a board's serial line to cxforth-server.
// Datagrams the board frames on the line (see wire/serial.go) go out as
// UDP, the replies go back framed, and everything else on the line is
// console text, copied to stdout.
//
//	cxforth-relay -line 127.0.0.1:5555 -server 127.0.0.1:7400   QEMU with -serial tcp::5555,server=on
//	cxforth-relay -dev /dev/ttyUSB0 -server 127.0.0.1:7400      a board, after stty -F /dev/ttyUSB0 115200 raw
//
// -type sends a line to the board once the console shows -after, for
// example to start the agent from the REPL.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/skycoin/skycoin-cx-forth/wire"
)

func main() {
	line := flag.String("line", "", "TCP address of the serial line, as QEMU serves it")
	dev := flag.String("dev", "", "serial device to open instead of -line")
	server := flag.String("server", "127.0.0.1:7400", "cxforth-server UDP address")
	typ := flag.String("type", "", "line to send to the board once -after shows")
	after := flag.String("after", "run one with", "console text that triggers -type")
	flag.Parse()

	rw, err := open(*line, *dev)
	if err != nil {
		fatal(err)
	}
	addr, err := net.ResolveUDPAddr("udp", *server)
	if err != nil {
		fatal(err)
	}
	uc, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		fatal(err)
	}

	con := &console{w: os.Stdout, after: []byte(*after), typ: *typ, line: rw}
	relay := wire.NewRelay(rw, con)
	var mu sync.Mutex
	go func() {
		buf := make([]byte, 64<<10)
		for {
			n, err := uc.Read(buf)
			if err != nil {
				// A refused send leaves an error here; the board retries.
				time.Sleep(100 * time.Millisecond)
				continue
			}
			mu.Lock()
			err = relay.Send(buf[:n])
			mu.Unlock()
			if err != nil {
				fmt.Fprintln(os.Stderr, "cxforth-relay:", err)
			}
		}
	}()
	err = relay.Run(func(b []byte) {
		if _, err := uc.Write(b); err != nil {
			fmt.Fprintln(os.Stderr, "cxforth-relay:", err)
		}
	})
	fatal(err)
}

func open(line, dev string) (io.ReadWriter, error) {
	switch {
	case dev != "":
		return os.OpenFile(dev, os.O_RDWR, 0)
	case line != "":
		var err error
		for range 50 {
			var c net.Conn
			if c, err = net.Dial("tcp", line); err == nil {
				return c, nil
			}
			time.Sleep(200 * time.Millisecond)
		}
		return nil, err
	}
	return nil, errors.New("give -line or -dev")
}

// console copies text to w and types once it has seen after.
type console struct {
	w     io.Writer
	after []byte
	typ   string
	line  io.Writer
	tail  []byte
	done  bool
}

func (c *console) Write(p []byte) (int, error) {
	if c.typ != "" && !c.done {
		c.tail = append(c.tail, p...)
		if bytes.Contains(c.tail, c.after) {
			c.done = true
			go func() {
				time.Sleep(200 * time.Millisecond)
				_, _ = io.WriteString(c.line, strings.TrimRight(c.typ, "\r\n")+"\r")
			}()
		}
		if len(c.tail) > 4096 {
			c.tail = c.tail[len(c.tail)-len(c.after):]
		}
	}
	return c.w.Write(p)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "cxforth-relay:", err)
	os.Exit(1)
}
