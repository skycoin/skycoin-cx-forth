package host

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/skycoin/skycoin-cx-forth/forth"
)

const updates = `{"ok":true,"result":[{"update_id":7,"message":{"chat":{"id":-42},"text":"hi \"you\" é😀"}},
 {"update_id":8 , "message" : { "chat":{"id":5}, "text":"two"} }]}`

func TestGet(t *testing.T) {
	cases := []struct{ path, want string }{
		{"", strings.TrimSpace(updates)},
		{"ok", "true"},
		{"result.0.update_id", "7"},
		{"result.1.message.text", `"two"`},
		{"result.0.message.chat.id", "-42"},
		{"result.2", ""},
		{"result.x", ""},
		{"nope", ""},
		{"ok.deeper", ""},
	}
	for _, c := range cases {
		if got := Get(updates, c.path); got != c.want {
			t.Errorf("Get(%q) = %q, want %q", c.path, got, c.want)
		}
	}
	if s, ok := Unquote(Get(updates, "result.0.message.text")); !ok || s != "hi \"you\" é😀" {
		t.Errorf("Unquote = %q", s)
	}
	if n := Len(updates, "result"); n != 2 {
		t.Errorf("Len(result) = %d", n)
	}
	if n := Len(updates, "result.1.message"); n != 2 {
		t.Errorf("Len(object) = %d", n)
	}
	for _, bad := range []string{"", "{", `{"a":}`, `{"a" 1}`, `[1,]`, `"x`, "\"a\x01\"", "tru"} {
		if Get(bad, "") != "" || Len(bad, "") != 0 {
			t.Errorf("malformed %q accepted", bad)
		}
	}
}

func TestSet(t *testing.T) {
	steps := []struct{ doc, path, raw, want string }{
		{"{}", "chat_id", "42", `{"chat_id":42}`},
		{`{"chat_id":42}`, "text", `"hi"`, `{"chat_id":42,"text":"hi"}`},
		{`{"a": {"b": 1}}`, "a.b", "[2]", `{"a": {"b": [2]}}`},
		{`{"a":1}`, "x.y.0", "true", `{"a":1,"x":{"y":[true]}}`},
		{"", "0", `{"r":1}`, `[{"r":1}]`},
		{"[1, 2]", "2", "3", "[1, 2,3]"},
		{"[1]", "0", "9", "[9]"},
		{"null", "k", "1", `{"k":1}`},
		{`{"a":1}`, "", "[]", "[]"},
	}
	for _, s := range steps {
		got, err := Set(s.doc, s.path, s.raw)
		if err != nil || got != s.want {
			t.Errorf("Set(%q, %q, %q) = %q, %v; want %q", s.doc, s.path, s.raw, got, err, s.want)
		}
	}
	for _, s := range []struct{ doc, path, raw string }{
		{"[1]", "5", "1"}, {`{"a":1}`, "a.b", "1"}, {"{}", "a", "nope"}, {"{", "a", "1"}, {"{}", "a", "1 2"},
	} {
		if got, err := Set(s.doc, s.path, s.raw); err == nil {
			t.Errorf("Set(%q, %q, %q) = %q, want an error", s.doc, s.path, s.raw, got)
		}
	}
	msgs, _ := Push("[]", "", `{"role":"user"}`)
	msgs, _ = Push(msgs, "", `{"role":"assistant"}`)
	if msgs != `[{"role":"user"},{"role":"assistant"}]` {
		t.Errorf("Push = %q", msgs)
	}
	if got, _ := Push(`{}`, "m", "1"); got != `{"m":[1]}` {
		t.Errorf("Push into missing = %q", got)
	}
}

func TestQuote(t *testing.T) {
	for _, s := range []string{"", "plain", "a\"b\\c\nd\te\x01f", "é😀", "\x7f"} {
		var back string
		if err := json.Unmarshal([]byte(Quote(s)), &back); err != nil || back != s {
			t.Errorf("Quote(%q) = %s, decodes to %q %v", s, Quote(s), back, err)
		}
		if got, ok := Unquote(Quote(s)); !ok || got != s {
			t.Errorf("Unquote(Quote(%q)) = %q", s, got)
		}
	}
}

func TestWords(t *testing.T) {
	var out strings.Builder
	vm := forth.New(&out)
	pins := MemPins{}
	Install(vm, &Env{Pins: pins, Now: func() int64 { return 1234 }, Sleep: func(int64) {}})
	src := `S\" {\"a\":[1,{\"b\":\"x\\ny\"}]}" DUP S" a.1.b" json.str TYPE
DUP S" a" json.len . DUP S" a.0" json.int . S" a.2" S" 7" json.set TYPE
S" k" S" v" kv.set S" k" kv.get TYPE S" zz" kv.get TYPE
3 1 gpio.set 3 gpio.get . time.now . S\" q\"" json.quote TYPE`
	if err := vm.Interpret(src); err != nil {
		t.Fatal(err)
	}
	want := "x\ny2 1 {\"a\":[1,{\"b\":\"x\\ny\"},7]}v1 1234 \"q\\\"\""
	if out.String() != want {
		t.Fatalf("got %q\nwant %q", out.String(), want)
	}
	if err := vm.Interpret(`S" GET" S" x" S" " net.request`); err == nil {
		t.Fatal("net.request without a Net worked")
	}
	if err := vm.Interpret(`S" [1]" S" 9" S" 1" json.set`); err == nil {
		t.Fatal("bad json.set worked")
	}
}

// Whatever the input, the helpers never panic. On a valid document a
// successful Set gives valid JSON that reads back the value, and Get agrees
// with encoding/json.
func FuzzJSON(f *testing.F) {
	f.Add(updates, "result.0.message.text", `"x"`)
	f.Add(`{"a":[1,2,{"b":null}]}`, "a.2.b", "[1]")
	f.Add(`[]`, "0", "{}")
	f.Add(`{"a.b":1}`, "a", "2")
	f.Fuzz(func(t *testing.T, doc, path, raw string) {
		got := Get(doc, path)
		Len(doc, path)
		out, err := Set(doc, path, raw)
		// encoding/json rewrites invalid UTF-8, so it is no oracle there.
		if !json.Valid([]byte(doc)) || !utf8.ValidString(doc) || !utf8.ValidString(path) {
			return
		}
		if got != "" {
			want := lookup(t, doc, path)
			var g any
			if json.Unmarshal([]byte(got), &g) != nil || !reflect.DeepEqual(g, want) {
				t.Fatalf("Get(%q, %q) = %q, encoding/json says %v", doc, path, got, want)
			}
		}
		if err != nil {
			return
		}
		if !json.Valid([]byte(out)) {
			t.Fatalf("Set(%q, %q, %q) = %q, not valid", doc, path, raw, out)
		}
		if strings.Contains(path, "..") || strings.HasPrefix(path, ".") || strings.HasSuffix(path, ".") {
			return
		}
		if Get(out, path) != strings.TrimSpace(raw) && path != "" {
			t.Fatalf("Set(%q, %q, %q) = %q, reads back %q", doc, path, raw, out, Get(out, path))
		}
	})
}

// lookup walks path with encoding/json. With duplicate keys it keeps the
// last one while Get finds the first, so those documents are skipped.
func lookup(t *testing.T, doc, path string) any {
	var v any
	dec := json.NewDecoder(strings.NewReader(doc))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		t.Skip()
	}
	if path == "" {
		return normalize(v)
	}
	for _, seg := range strings.Split(path, ".") {
		switch x := v.(type) {
		case map[string]any:
			v = x[seg]
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(x) {
				t.Fatalf("Get found %q in %q", path, doc)
			}
			v = x[i]
		}
	}
	return normalize(v)
}

func normalize(v any) any {
	b, _ := json.Marshal(v)
	var out any
	_ = json.Unmarshal(b, &out)
	return out
}
