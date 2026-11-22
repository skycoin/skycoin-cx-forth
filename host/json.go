package host

import (
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"
)

// JSON helpers that work on the text itself, without reflection or a
// document tree, so they stay small under TinyGo. A path is keys and array
// indexes joined by dots, such as result.0.message.text, and the empty
// path is the whole document. Keys that contain a dot cannot be named.

const maxDepth = 64

var errJSON = errors.New("bad json")

func ws(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i++
	}
	return i
}

// value returns the end of the value starting at s[i].
func value(s string, i, depth int) (int, error) {
	if i >= len(s) || depth > maxDepth {
		return 0, errJSON
	}
	switch c := s[i]; {
	case c == '"':
		return str(s, i)
	case c == '{' || c == '[':
		close := byte('}')
		if c == '[' {
			close = ']'
		}
		i = ws(s, i+1)
		if i < len(s) && s[i] == close {
			return i + 1, nil
		}
		for {
			var err error
			if c == '{' {
				if i, err = str(s, i); err != nil {
					return 0, err
				}
				if i = ws(s, i); i >= len(s) || s[i] != ':' {
					return 0, errJSON
				}
				i = ws(s, i+1)
			}
			if i, err = value(s, i, depth+1); err != nil {
				return 0, err
			}
			i = ws(s, i)
			switch {
			case i < len(s) && s[i] == ',':
				i = ws(s, i+1)
			case i < len(s) && s[i] == close:
				return i + 1, nil
			default:
				return 0, errJSON
			}
		}
	case c == '-' || (c >= '0' && c <= '9'):
		return number(s, i)
	}
	for _, lit := range []string{"true", "false", "null"} {
		if strings.HasPrefix(s[i:], lit) {
			return i + len(lit), nil
		}
	}
	return 0, errJSON
}

// str returns the end of the string starting at s[i].
func str(s string, i int) (int, error) {
	if i >= len(s) || s[i] != '"' {
		return 0, errJSON
	}
	for i++; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			return i + 1, nil
		default:
			if s[i] < 0x20 {
				return 0, errJSON
			}
		}
	}
	return 0, errJSON
}

// find returns the span of the value at path, or ok false if it is absent.
func find(doc, path string) (a, b int, ok bool) {
	a = ws(doc, 0)
	b, err := value(doc, a, 0)
	if err != nil {
		return 0, 0, false
	}
	if path == "" {
		return a, b, true
	}
	for _, seg := range strings.Split(path, ".") {
		if a, b, ok = child(doc, a, seg); !ok {
			return 0, 0, false
		}
	}
	return a, b, true
}

// each calls fn for every element of the object or array at doc[a], with
// its index, its key in an object, and the span of its value, until fn
// returns false. The value at doc[a] is already known to be well formed.
func each(doc string, a int, fn func(n int, key string, va, vb int) bool) {
	obj := doc[a] == '{'
	i := ws(doc, a+1)
	for n := 0; doc[i] != '}' && doc[i] != ']'; n++ {
		var key string
		if obj {
			e, _ := str(doc, i)
			key, _ = Unquote(doc[i:e])
			i = ws(doc, ws(doc, e)+1)
		}
		e, _ := value(doc, i, 0)
		if !fn(n, key, i, e) {
			return
		}
		i = ws(doc, e)
		if doc[i] == ',' {
			i = ws(doc, i+1)
		}
	}
}

// child finds seg in the object or array at doc[a].
func child(doc string, a int, seg string) (ca, cb int, ok bool) {
	if doc[a] != '{' && doc[a] != '[' {
		return 0, 0, false
	}
	obj := doc[a] == '{'
	idx, err := strconv.Atoi(seg)
	if !obj && (err != nil || idx < 0) {
		return 0, 0, false
	}
	each(doc, a, func(n int, key string, va, vb int) bool {
		if (obj && key == seg) || (!obj && n == idx) {
			ca, cb, ok = va, vb, true
		}
		return !ok
	})
	return ca, cb, ok
}

// Get returns the raw JSON text at path, or "" if it is absent.
func Get(doc, path string) string {
	a, b, ok := find(doc, path)
	if !ok {
		return ""
	}
	return doc[a:b]
}

// Len returns the number of elements of the array or object at path, and
// 0 for anything else.
func Len(doc, path string) int {
	a, _, ok := find(doc, path)
	if !ok || (doc[a] != '[' && doc[a] != '{') {
		return 0
	}
	n := 0
	each(doc, a, func(int, string, int, int) bool { n++; return true })
	return n
}

// Set returns doc with the value at path replaced by raw, creating objects
// and arrays on the way as needed. An array index may be at most the
// length, which appends.
func Set(doc, path, raw string) (string, error) {
	if b, err := value(raw, ws(raw, 0), 0); err != nil || ws(raw, b) != len(raw) {
		return "", errors.New("json.set: bad value")
	}
	var segs []string
	if path != "" {
		segs = strings.Split(path, ".")
	}
	return set(doc, segs, raw)
}

func set(doc string, segs []string, raw string) (string, error) {
	if len(segs) == 0 {
		return raw, nil
	}
	seg := segs[0]
	idx, err := strconv.Atoi(seg)
	array := err == nil && idx >= 0
	a := ws(doc, 0)
	if a == len(doc) || strings.HasPrefix(doc[a:], "null") {
		doc, a = "{}", 0
		if array {
			doc = "[]"
		}
	}
	b, err := value(doc, a, 0)
	if err != nil || ws(doc, b) != len(doc) || (doc[a] != '{' && doc[a] != '[') {
		return "", errors.New("json.set: not an object or array at " + seg)
	}
	if ca, cb, ok := child(doc, a, seg); ok {
		v, err := set(doc[ca:cb], segs[1:], raw)
		if err != nil {
			return "", err
		}
		return doc[:ca] + v + doc[cb:], nil
	}
	if doc[a] == '[' && (!array || idx != Len(doc[a:b], "")) {
		return "", errors.New("json.set: index out of range: " + seg)
	}
	v, err := set("", segs[1:], raw)
	if err != nil {
		return "", err
	}
	if doc[a] == '{' {
		v = Quote(seg) + ":" + v
	}
	end := b - 1 // the closing bracket
	if ws(doc, a+1) != end {
		v = "," + v
	}
	return doc[:end] + v + doc[end:], nil
}

// Push appends raw to the array at path, creating it if absent.
func Push(doc, path, raw string) (string, error) {
	p := strconv.Itoa(Len(doc, path))
	if path != "" {
		p = path + "." + p
	}
	return Set(doc, p, raw)
}

// Quote returns s as a JSON string.
func Quote(s string) string {
	b := make([]byte, 0, len(s)+2)
	b = append(b, '"')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '"' || c == '\\':
			b = append(b, '\\', c)
		case c == '\n':
			b = append(b, '\\', 'n')
		case c == '\r':
			b = append(b, '\\', 'r')
		case c == '\t':
			b = append(b, '\\', 't')
		case c < 0x20:
			b = append(b, '\\', 'u', '0', '0', "0123456789abcdef"[c>>4], "0123456789abcdef"[c&15])
		default:
			b = append(b, c)
		}
	}
	return string(append(b, '"'))
}

// Unquote decodes a JSON string. For any other value it returns the text
// unchanged and false.
func Unquote(raw string) (string, bool) {
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return raw, false
	}
	s := raw[1 : len(raw)-1]
	if strings.IndexByte(s, '\\') < 0 {
		return s, true
	}
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 >= len(s) {
			b = append(b, c)
			continue
		}
		i++
		switch s[i] {
		case 'n':
			b = append(b, '\n')
		case 'r':
			b = append(b, '\r')
		case 't':
			b = append(b, '\t')
		case 'b':
			b = append(b, '\b')
		case 'f':
			b = append(b, '\f')
		case 'u':
			r, n := hex4(s, i+1)
			i += n
			if r >= 0xd800 && r < 0xdc00 && i+2 < len(s) && s[i+1] == '\\' && s[i+2] == 'u' {
				if r2, n2 := hex4(s, i+3); r2 >= 0xdc00 && r2 < 0xe000 {
					r = 0x10000 + (r-0xd800)<<10 + (r2 - 0xdc00)
					i += 2 + n2
				}
			}
			b = utf8.AppendRune(b, r)
		default:
			b = append(b, s[i])
		}
	}
	return string(b), true
}

func hex4(s string, i int) (rune, int) {
	if i+4 > len(s) {
		return utf8.RuneError, len(s) - i
	}
	n, err := strconv.ParseUint(s[i:i+4], 16, 32)
	if err != nil {
		return utf8.RuneError, 4
	}
	return rune(n), 4
}

// number returns the end of the number starting at s[i], following the
// grammar in RFC 8259 section 6.
func number(s string, i int) (int, error) {
	digits := func(i int) int {
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		return i
	}
	if s[i] == '-' {
		i++
	}
	switch {
	case i < len(s) && s[i] == '0':
		i++
	case i < len(s) && s[i] >= '1' && s[i] <= '9':
		i = digits(i)
	default:
		return 0, errJSON
	}
	if i < len(s) && s[i] == '.' {
		j := digits(i + 1)
		if j == i+1 {
			return 0, errJSON
		}
		i = j
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			i++
		}
		j := digits(i)
		if j == i {
			return 0, errJSON
		}
		i = j
	}
	return i, nil
}
