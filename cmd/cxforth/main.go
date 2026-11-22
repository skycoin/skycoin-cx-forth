// Command cxforth is a Forth REPL that can also run the CX IR examples.
//
//	cxforth                 REPL on stdin
//	cxforth file.fs ...     load Forth files, then the REPL unless -e or -cx ran
//	cxforth -e '1 2 + .'    evaluate a line
//	cxforth -cx fib         run a built in CX IR example
//	cxforth -cx fib -emit   print the Forth the example translates to
//	cxforth -list           list the examples
//	cxforth -agent          run the chat agent against cxforth-server, key in CXFORTH_KEY
//	cxforth -agent -emit    print the Forth the agent translates to
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"

	"github.com/skycoin/skycoin-cx-forth/cxir"
	"github.com/skycoin/skycoin-cx-forth/forth"
)

func main() {
	eval := flag.String("e", "", "evaluate Forth source and exit")
	cx := flag.String("cx", "", "run a built in CX IR example")
	emit := flag.Bool("emit", false, "with -cx, print the translated Forth instead of running it")
	list := flag.Bool("list", false, "list the CX IR examples")
	quiet := flag.Bool("q", false, "REPL without the ok prompt")
	runAgent := flag.Bool("agent", false, "run the chat agent")
	server := flag.String("server", "127.0.0.1:7400", "with -agent, the cxforth-server UDP address")
	flag.Parse()

	if *runAgent {
		if err := agentMain(*server, *emit); err != nil {
			fmt.Fprintln(os.Stderr, "cxforth:", err)
			os.Exit(1)
		}
		return
	}

	if *list {
		for _, ex := range cxir.Examples {
			fmt.Printf("%s\n%s\n\n", ex.Name, ex.Source)
		}
		return
	}

	out := bufio.NewWriter(os.Stdout)
	defer func() { _ = out.Flush() }()
	vm := forth.New(out)
	for _, path := range flag.Args() {
		src, err := os.ReadFile(path)
		if err != nil {
			fatal(out, err)
		}
		if err := vm.Interpret(string(src)); err != nil {
			fatal(out, fmt.Errorf("%s: %w", path, err))
		}
	}

	if *cx != "" {
		ex := find(*cx)
		if ex == nil {
			fatal(out, fmt.Errorf("no example %q, try -list", *cx))
		}
		if *emit {
			src, err := cxir.Translate(&ex.Program)
			if err != nil {
				fatal(out, err)
			}
			_, _ = fmt.Fprint(out, src)
			return
		}
		if err := cxir.Load(vm, &ex.Program); err != nil {
			fatal(out, err)
		}
		if err := vm.Interpret(cxir.WordName("main")); err != nil {
			fatal(out, err)
		}
		return
	}
	if *eval != "" {
		if err := vm.Interpret(*eval); err != nil {
			fatal(out, err)
		}
		_, _ = fmt.Fprintln(out)
		return
	}

	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		if err := vm.Interpret(sc.Text()); err != nil {
			_, _ = fmt.Fprintf(out, " error: %v\n", err)
		} else if !*quiet {
			_, _ = fmt.Fprintln(out, " ok")
		}
		_ = out.Flush()
	}
}

func find(name string) *cxir.Example {
	for i := range cxir.Examples {
		if cxir.Examples[i].Name == name {
			return &cxir.Examples[i]
		}
	}
	return nil
}

func fatal(out *bufio.Writer, err error) {
	_ = out.Flush()
	fmt.Fprintln(os.Stderr, "cxforth:", err)
	os.Exit(1)
}
