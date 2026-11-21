// Package mock fakes the Telegram Bot API and an OpenAI style chat
// completions endpoint, so the agent runs end to end without accounts.
package mock

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Sent is a message the bot sent.
type Sent struct {
	ChatID int64
	Text   string
	At     time.Time
}

// Telegram serves getUpdates with long polling and records sendMessage.
type Telegram struct {
	Token string
	// MaxWait caps the long poll timeout a client asks for.
	MaxWait time.Duration

	srv    *httptest.Server
	mu     sync.Mutex
	queue  []json.RawMessage
	first  int // update_id of queue[0]
	notify chan struct{}
	sent   chan Sent
	polls  int
}

// NewTelegram starts a fake Bot API that accepts token.
func NewTelegram(token string) *Telegram {
	t := &Telegram{Token: token, MaxWait: 2 * time.Second, first: 1, notify: make(chan struct{}), sent: make(chan Sent, 64)}
	t.srv = httptest.NewServer(http.HandlerFunc(t.serve))
	return t
}

// URL is the API base, the part before /bot<token>.
func (t *Telegram) URL() string { return t.srv.URL }

// Close stops the server.
func (t *Telegram) Close() { t.srv.Close() }

// Sent delivers the messages the bot sends.
func (t *Telegram) Sent() <-chan Sent { return t.sent }

// Polls counts getUpdates calls.
func (t *Telegram) Polls() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.polls
}

// Say queues a user message and returns when it was queued.
func (t *Telegram) Say(chatID int64, text string) time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	id := t.first + len(t.queue)
	u, _ := json.Marshal(map[string]any{
		"update_id": id,
		"message": map[string]any{
			"message_id": id,
			"chat":       map[string]any{"id": chatID, "type": "private"},
			"from":       map[string]any{"id": chatID, "first_name": "Test"},
			"date":       time.Now().Unix(),
			"text":       text,
		},
	})
	t.queue = append(t.queue, u)
	close(t.notify)
	t.notify = make(chan struct{})
	return time.Now()
}

func (t *Telegram) serve(w http.ResponseWriter, r *http.Request) {
	prefix := "/bot" + t.Token + "/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		reply(w, 401, map[string]any{"ok": false, "error_code": 401, "description": "Unauthorized"})
		return
	}
	switch r.URL.Path[len(prefix):] {
	case "getUpdates":
		t.getUpdates(w, r)
	case "sendMessage":
		var m struct {
			ChatID json.Number `json:"chat_id"`
			Text   string      `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&m); err != nil || m.Text == "" {
			reply(w, 400, map[string]any{"ok": false, "description": "Bad Request: message text is empty"})
			return
		}
		id, _ := m.ChatID.Int64()
		t.sent <- Sent{ChatID: id, Text: m.Text, At: time.Now()}
		reply(w, 200, map[string]any{"ok": true, "result": map[string]any{"message_id": 1}})
	default:
		reply(w, 404, map[string]any{"ok": false, "description": "Not Found"})
	}
}

func (t *Telegram) getUpdates(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	offset, _ := strconv.Atoi(q.Get("offset"))
	secs, _ := strconv.Atoi(q.Get("timeout"))
	deadline := time.Now().Add(min(time.Duration(secs)*time.Second, t.MaxWait))
	for {
		t.mu.Lock()
		t.polls++
		// Asking for offset confirms and forgets every earlier update.
		if drop := offset - t.first; drop > 0 {
			drop = min(drop, len(t.queue))
			t.queue = t.queue[drop:]
			t.first += drop
		}
		res := append([]json.RawMessage{}, t.queue...)
		wake := t.notify
		t.mu.Unlock()
		left := time.Until(deadline)
		if len(res) > 0 || left <= 0 {
			reply(w, 200, map[string]any{"ok": true, "result": res})
			return
		}
		select {
		case <-wake:
		case <-time.After(left):
		case <-r.Context().Done():
			return
		}
	}
}

// LLM answers chat completion requests from a script. A user message
// containing "led on" or "led off" calls gpio_write on pin 2, "remember K V"
// calls memory_set, "recall K" calls memory_get and "time" calls get_time.
// After a tool result it answers "done: <result>". Anything else is echoed.
type LLM struct {
	Key string

	srv  *httptest.Server
	mu   sync.Mutex
	reqs []json.RawMessage
}

// NewLLM starts a fake endpoint that wants key as its bearer token.
func NewLLM(key string) *LLM {
	l := &LLM{Key: key}
	l.srv = httptest.NewServer(http.HandlerFunc(l.serve))
	return l
}

// URL is the chat completions URL.
func (l *LLM) URL() string { return l.srv.URL + "/v1/chat/completions" }

// Close stops the server.
func (l *LLM) Close() { l.srv.Close() }

// Requests returns the request bodies received so far.
func (l *LLM) Requests() []json.RawMessage {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]json.RawMessage{}, l.reqs...)
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (l *LLM) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v1/chat/completions" || r.Method != "POST" {
		reply(w, 404, map[string]any{"error": map[string]any{"message": "not found"}})
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+l.Key {
		reply(w, 401, map[string]any{"error": map[string]any{"message": "bad key"}})
		return
	}
	var raw json.RawMessage
	var req struct {
		Model    string            `json:"model"`
		Messages []chatMessage     `json:"messages"`
		Tools    []json.RawMessage `json:"tools"`
	}
	if json.NewDecoder(r.Body).Decode(&raw) != nil || json.Unmarshal(raw, &req) != nil || len(req.Messages) == 0 {
		reply(w, 400, map[string]any{"error": map[string]any{"message": "bad request"}})
		return
	}
	l.mu.Lock()
	l.reqs = append(l.reqs, raw)
	n := len(l.reqs)
	l.mu.Unlock()

	last := req.Messages[len(req.Messages)-1]
	msg := map[string]any{"role": "assistant", "content": nil}
	call := func(name string, args map[string]any) {
		a, _ := json.Marshal(args)
		msg["tool_calls"] = []any{map[string]any{
			"id": "call_" + strconv.Itoa(n), "type": "function",
			"function": map[string]any{"name": name, "arguments": string(a)},
		}}
	}
	words := strings.Fields(last.Content)
	switch {
	case last.Role == "tool":
		msg["content"] = "done: " + last.Content
	case strings.Contains(last.Content, "led on"):
		call("gpio_write", map[string]any{"pin": 2, "state": 1})
	case strings.Contains(last.Content, "led off"):
		call("gpio_write", map[string]any{"pin": 2, "state": 0})
	case len(words) == 3 && words[0] == "remember":
		call("memory_set", map[string]any{"key": words[1], "value": words[2]})
	case len(words) == 2 && words[0] == "recall":
		call("memory_get", map[string]any{"key": words[1]})
	case last.Content == "time":
		call("get_time", map[string]any{})
	default:
		msg["content"] = "echo: " + last.Content
	}
	finish := "stop"
	if msg["tool_calls"] != nil {
		finish = "tool_calls"
	}
	reply(w, 200, map[string]any{
		"id": "chatcmpl-" + strconv.Itoa(n), "object": "chat.completion", "model": req.Model,
		"choices": []any{map[string]any{"index": 0, "message": msg, "finish_reason": finish}},
	})
}

func reply(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
