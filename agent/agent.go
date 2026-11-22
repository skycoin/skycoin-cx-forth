// Package agent is a small chat agent written as CX IR, after zclaw
// (github.com/tnm/zclaw). It polls Telegram for messages, asks an LLM with
// a set of tools, runs the tool calls the LLM makes and sends the reply.
// Every outside call goes through the words of package host, and the
// network ones through the gateway, so the program holds no credentials.
//
// The CX it stands for, in outline:
//
//	func main() {
//		for polls == 0 || n < polls {
//			st, body := net.request("GET", "tg/getUpdates?timeout=..&offset=..", "")
//			for each update { offset = update_id + 1; handle(update) }
//		}
//	}
//	func handle(upd str) { reply := ask(text); net.request("POST", "tg/sendMessage", ..) }
//	func ask(text str) (reply str) {
//		for round < 5 { resp := llm(messages); if no tool calls { return content }
//			for each call { messages += tool result } }
//	}
//	func tool(name str, args str) (result str) { switch name { .. } }
//
// It reads its settings with kv.get: agent.polls (0 or unset runs forever),
// tg.timeout (long poll seconds, 20 when unset), llm.model and agent.system.
package agent

import (
	"github.com/skycoin/skycoin-cx-forth/cxir"
	"github.com/skycoin/skycoin-cx-forth/forth"
	"github.com/skycoin/skycoin-cx-forth/host"
)

// Tools is the tool list sent to the LLM, in the OpenAI format.
const Tools = `[` +
	`{"type":"function","function":{"name":"gpio_write","description":"Set a GPIO pin high (1) or low (0).","parameters":{"type":"object","properties":{"pin":{"type":"integer"},"state":{"type":"integer"}},"required":["pin","state"]}}},` +
	`{"type":"function","function":{"name":"gpio_read","description":"Read a GPIO pin.","parameters":{"type":"object","properties":{"pin":{"type":"integer"}},"required":["pin"]}}},` +
	`{"type":"function","function":{"name":"memory_set","description":"Remember a value under a key.","parameters":{"type":"object","properties":{"key":{"type":"string"},"value":{"type":"string"}},"required":["key","value"]}}},` +
	`{"type":"function","function":{"name":"memory_get","description":"Recall the value stored under a key.","parameters":{"type":"object","properties":{"key":{"type":"string"}},"required":["key"]}}},` +
	`{"type":"function","function":{"name":"get_time","description":"Milliseconds on the device clock.","parameters":{"type":"object","properties":{}}}}` +
	`]`

// System is the default system prompt.
const System = "You are a small agent running as CX on a Forth VM. Use the tools to act on the device. Reply in one or two short sentences."

// MaxRounds bounds the LLM calls for one message, as zclaw's MAX_TOOL_ROUNDS.
const MaxRounds = 5

var (
	hcall = cxir.Host
	lit   = cxir.Str
	num   = cxir.Int64
)

func s(n string) cxir.Arg { return cxir.Var(n, cxir.STR) }
func i(n string) cxir.Arg { return cxir.Var(n, cxir.I64) }
func b(n string) cxir.Arg { return cxir.Var(n, cxir.BOOL) }

func let(out, in cxir.Arg) cxir.Expr   { return cxir.Op1(cxir.OpIdentity, out, in) }
func cat(out, x, y cxir.Arg) cxir.Expr { return cxir.Op1(cxir.OpAdd, out, x, y) }
func eq(out, x, y cxir.Arg) cxir.Expr  { return cxir.Op1(cxir.OpEqual, out, x, y) }
func str(out, x cxir.Arg) cxir.Expr    { return hcall(">STR", []cxir.Arg{out}, x) }
func jget(out, doc, path cxir.Arg) cxir.Expr {
	return hcall("json.get", []cxir.Arg{out}, doc, path)
}
func jstr(out, doc, path cxir.Arg) cxir.Expr {
	return hcall("json.str", []cxir.Arg{out}, doc, path)
}
func jint(out, doc, path cxir.Arg) cxir.Expr {
	return hcall("json.int", []cxir.Arg{out}, doc, path)
}
func jset(out, doc, path, raw cxir.Arg) cxir.Expr {
	return hcall("json.set", []cxir.Arg{out}, doc, path, raw)
}
func jpush(out, doc, raw cxir.Arg) cxir.Expr {
	return hcall("json.push", []cxir.Arg{out}, doc, lit(""), raw)
}
func quote(out, x cxir.Arg) cxir.Expr { return hcall("json.quote", []cxir.Arg{out}, x) }
func request(st, body, method, path, payload cxir.Arg) cxir.Expr {
	return hcall("net.request", []cxir.Arg{st, body}, method, path, payload)
}

// builders makes the functions, callees first.
var builders = []func() (cxir.Function, error){toolFn, askFn, handleFn, mainFn}

// Program returns the agent. Its entry point is main.
func Program() (*cxir.Program, error) {
	var fns []cxir.Function
	for _, f := range builders {
		fn, err := f()
		if err != nil {
			return nil, err
		}
		fns = append(fns, fn)
	}
	return &cxir.Program{Functions: fns}, nil
}

func mainFn() (cxir.Function, error) {
	polls, n, offset, timeout, st, cnt, k := i("polls"), i("n"), i("offset"), i("timeout"), i("st"), i("cnt"), i("k")
	url, body, upd, tmp, ks, c := s("url"), s("body"), s("upd"), s("tmp"), s("ks"), b("c")
	var l cxir.Lines
	l.Add(
		hcall("kv.get", []cxir.Arg{tmp}, lit("agent.polls")),
		jint(polls, tmp, lit("")),
		hcall("kv.get", []cxir.Arg{tmp}, lit("tg.timeout")),
		jint(timeout, tmp, lit("")),
		eq(c, timeout, num(0)),
	)
	l.If(c, "", "poll")
	l.Add(let(timeout, num(20)))
	l.Label("poll")
	l.Add(cxir.Op1(cxir.OpGt, c, polls, num(0)))
	l.If(c, "", "go")
	l.Add(cxir.Op1(cxir.OpGteq, c, n, polls))
	l.If(c, "end", "")
	l.Label("go")
	l.Add(
		cxir.Op1(cxir.OpAdd, n, n, num(1)),
		str(ks, timeout),
		cat(url, lit("tg/getUpdates?timeout="), ks),
		str(ks, offset),
		cat(url, url, lit("&offset=")),
		cat(url, url, ks),
		request(st, body, lit("GET"), url, lit("")),
		eq(c, st, num(200)),
	)
	l.If(c, "got", "")
	l.Add(
		str(ks, st),
		cat(tmp, lit("getUpdates failed: "), ks),
		cxir.Print(tmp),
		hcall("sleep", nil, num(1000)),
	)
	l.Goto("poll")
	l.Label("got")
	l.Add(hcall("json.len", []cxir.Arg{cnt}, body, lit("result")), let(k, num(0)))
	l.Label("each")
	l.Add(cxir.Op1(cxir.OpLt, c, k, cnt))
	l.If(c, "", "poll")
	l.Add(
		str(ks, k),
		cat(tmp, lit("result."), ks),
		jget(upd, body, tmp),
		jint(offset, upd, lit("update_id")),
		cxir.Op1(cxir.OpAdd, offset, offset, num(1)),
		cxir.Call("handle", nil, upd),
		cxir.Op1(cxir.OpAdd, k, k, num(1)),
	)
	l.Goto("each")
	l.Label("end")
	e, err := l.Exprs()
	return cxir.Function{
		Name:   "main",
		Locals: []cxir.Arg{polls, n, offset, timeout, st, cnt, k, url, body, upd, tmp, ks, c},
		Exprs:  e,
	}, err
}

func handleFn() (cxir.Function, error) {
	upd, chat, text, reply, p, q, resp, tmp, ks := s("upd"), s("chat"), s("text"), s("reply"), s("p"), s("q"), s("resp"), s("tmp"), s("ks")
	st, c := i("st"), b("c")
	var l cxir.Lines
	l.Add(
		jget(chat, upd, lit("message.chat.id")),
		jstr(text, upd, lit("message.text")),
		eq(c, text, lit("")),
	)
	l.If(c, "end", "")
	l.Add(
		cat(tmp, lit("< "), chat),
		cat(tmp, tmp, lit(": ")),
		cat(tmp, tmp, text),
		cxir.Print(tmp),
		cxir.Call("ask", []cxir.Arg{reply}, text),
		cat(tmp, lit("> "), reply),
		cxir.Print(tmp),
		jset(p, lit("{}"), lit("chat_id"), chat),
		quote(q, reply),
		jset(p, p, lit("text"), q),
		request(st, resp, lit("POST"), lit("tg/sendMessage"), p),
		eq(c, st, num(200)),
	)
	l.If(c, "end", "")
	l.Add(str(ks, st), cat(tmp, lit("sendMessage failed: "), ks), cxir.Print(tmp))
	l.Label("end")
	e, err := l.Exprs()
	return cxir.Function{
		Name:   "handle",
		Inputs: []cxir.Arg{upd},
		Locals: []cxir.Arg{chat, text, reply, p, q, resp, tmp, ks, st, c},
		Exprs:  e,
	}, err
}

func askFn() (cxir.Function, error) {
	text, reply, model, sys, req, msgs, m, q, resp, msg, call, name, args, result, tmp, ks := s("text"), s("reply"), s("model"), s("sys"), s("req"), s("msgs"), s("m"), s("q"), s("resp"), s("msg"), s("call"), s("name"), s("args"), s("result"), s("tmp"), s("ks")
	round, st, n, j, c := i("round"), i("st"), i("n"), i("j"), b("c")
	var l cxir.Lines
	l.Add(
		hcall("kv.get", []cxir.Arg{model}, lit("llm.model")),
		eq(c, model, lit("")),
	)
	l.If(c, "", "model")
	l.Add(let(model, lit("gpt-4o-mini")))
	l.Label("model")
	l.Add(
		hcall("kv.get", []cxir.Arg{sys}, lit("agent.system")),
		eq(c, sys, lit("")),
	)
	l.If(c, "", "sys")
	l.Add(let(sys, lit(System)))
	l.Label("sys")
	l.Add(
		quote(q, model),
		jset(req, lit("{}"), lit("model"), q),
		jset(req, req, lit("tools"), lit(Tools)),
		quote(q, sys),
		jset(m, lit(`{"role":"system"}`), lit("content"), q),
		jpush(msgs, lit("[]"), m),
		quote(q, text),
		jset(m, lit(`{"role":"user"}`), lit("content"), q),
		jpush(msgs, msgs, m),
	)
	l.Label("round")
	l.Add(cxir.Op1(cxir.OpGteq, c, round, num(MaxRounds)))
	l.If(c, "", "call")
	l.Add(let(reply, lit("Stopped after too many tool calls.")))
	l.Add(cxir.Return())
	l.Label("call")
	l.Add(
		cxir.Op1(cxir.OpAdd, round, round, num(1)),
		jset(req, req, lit("messages"), msgs),
		request(st, resp, lit("POST"), lit("llm"), req),
		eq(c, st, num(200)),
	)
	l.If(c, "ok", "")
	l.Add(str(ks, st), cat(reply, lit("LLM error "), ks), cxir.Return())
	l.Label("ok")
	l.Add(
		jget(msg, resp, lit("choices.0.message")),
		jpush(msgs, msgs, msg),
		hcall("json.len", []cxir.Arg{n}, msg, lit("tool_calls")),
		eq(c, n, num(0)),
	)
	l.If(c, "", "tools")
	l.Add(jstr(reply, msg, lit("content")), cxir.Return())
	l.Label("tools")
	l.Add(let(j, num(0)))
	l.Label("each")
	l.Add(cxir.Op1(cxir.OpLt, c, j, n))
	l.If(c, "", "round")
	l.Add(
		str(ks, j),
		cat(tmp, lit("tool_calls."), ks),
		jget(call, msg, tmp),
		jstr(name, call, lit("function.name")),
		jstr(args, call, lit("function.arguments")),
		cxir.Call("tool", []cxir.Arg{result}, name, args),
		cat(tmp, lit("  tool "), name),
		cat(tmp, tmp, lit(" ")),
		cat(tmp, tmp, args),
		cat(tmp, tmp, lit(" -> ")),
		cat(tmp, tmp, result),
		cxir.Print(tmp),
		jget(tmp, call, lit("id")),
		jset(m, lit(`{"role":"tool"}`), lit("tool_call_id"), tmp),
		quote(q, result),
		jset(m, m, lit("content"), q),
		jpush(msgs, msgs, m),
		cxir.Op1(cxir.OpAdd, j, j, num(1)),
	)
	l.Goto("each")
	e, err := l.Exprs()
	return cxir.Function{
		Name:    "ask",
		Inputs:  []cxir.Arg{text},
		Outputs: []cxir.Arg{reply},
		Locals:  []cxir.Arg{model, sys, req, msgs, m, q, resp, msg, call, name, args, result, tmp, ks, round, st, n, j, c},
		Exprs:   e,
	}, err
}

func toolFn() (cxir.Function, error) {
	name, args, result, key, val, ks := s("name"), s("args"), s("result"), s("key"), s("val"), s("ks")
	pin, level, c := i("pin"), i("level"), b("c")
	var l cxir.Lines
	l.Add(jint(pin, args, lit("pin")), eq(c, name, lit("gpio_write")))
	l.If(c, "", "read")
	l.Add(
		jint(level, args, lit("state")),
		hcall("gpio.set", nil, pin, level),
		str(ks, pin),
		cat(result, lit("pin "), ks),
		str(ks, level),
		cat(result, result, lit(" set to ")),
		cat(result, result, ks),
		cxir.Return(),
	)
	l.Label("read")
	l.Add(eq(c, name, lit("gpio_read")))
	l.If(c, "", "set")
	l.Add(
		hcall("gpio.get", []cxir.Arg{level}, pin),
		str(ks, pin),
		cat(result, lit("pin "), ks),
		str(ks, level),
		cat(result, result, lit(" is ")),
		cat(result, result, ks),
		cxir.Return(),
	)
	l.Label("set")
	l.Add(jstr(key, args, lit("key")), cat(key, lit("mem."), key), eq(c, name, lit("memory_set")))
	l.If(c, "", "get")
	l.Add(
		jstr(val, args, lit("value")),
		hcall("kv.set", nil, key, val),
		let(result, lit("stored")),
		cxir.Return(),
	)
	l.Label("get")
	l.Add(eq(c, name, lit("memory_get")))
	l.If(c, "", "time")
	l.Add(hcall("kv.get", []cxir.Arg{result}, key), eq(c, result, lit("")))
	l.If(c, "", "end")
	l.Add(let(result, lit("nothing stored")), cxir.Return())
	l.Label("time")
	l.Add(eq(c, name, lit("get_time")))
	l.If(c, "", "unknown")
	l.Add(hcall("time.now", []cxir.Arg{pin}), str(result, pin), cxir.Return())
	l.Label("unknown")
	l.Add(cat(result, lit("unknown tool "), name))
	l.Label("end")
	e, err := l.Exprs()
	return cxir.Function{
		Name:    "tool",
		Inputs:  []cxir.Arg{name, args},
		Outputs: []cxir.Arg{result},
		Locals:  []cxir.Arg{key, val, ks, pin, level, c},
		Exprs:   e,
	}, err
}

// Load defines the host words over e in vm and compiles the agent. Run it
// with vm.Interpret(cxir.WordName("main")).
func Load(vm *forth.VM, e *host.Env) error {
	host.Install(vm, e)
	// One function at a time keeps the peak small on a board.
	for _, build := range builders {
		f, err := build()
		if err != nil {
			return err
		}
		if err := cxir.LoadFunc(vm, &f); err != nil {
			return err
		}
	}
	return nil
}
