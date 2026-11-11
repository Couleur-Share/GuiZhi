package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// ---------- 最小 CDP 客户端 ----------

type cdpMsg struct {
	ID        int                       `json:"id"`
	Method    string                    `json:"method"`
	Params    json.RawMessage           `json:"params"`
	Result    json.RawMessage           `json:"result"`
	Error     *struct{ Message string } `json:"error"`
	SessionID string                    `json:"sessionId"`
}

type cdp struct {
	c       *websocket.Conn
	mu      sync.Mutex
	nextID  int
	pend    map[int]chan cdpMsg
	onEvent func(m cdpMsg)
}

func dialCDP(ctx context.Context, url string) (*cdp, error) {
	// 不带 Origin：DevTools 对浏览器来源的 WebSocket 会拒绝，非浏览器客户端不受影响。
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return nil, err
	}
	c.SetReadLimit(64 << 20)
	d := &cdp{c: c, pend: map[int]chan cdpMsg{}}
	go func() {
		for {
			_, b, err := c.Read(context.Background())
			if err != nil {
				return
			}
			var m cdpMsg
			if json.Unmarshal(b, &m) != nil {
				continue
			}
			if m.ID != 0 {
				d.mu.Lock()
				ch := d.pend[m.ID]
				delete(d.pend, m.ID)
				d.mu.Unlock()
				if ch != nil {
					ch <- m
				}
				continue
			}
			if d.onEvent != nil {
				d.onEvent(m)
			}
		}
	}()
	return d, nil
}

func (d *cdp) call(sid, method string, params any) (json.RawMessage, error) {
	d.mu.Lock()
	d.nextID++
	id := d.nextID
	ch := make(chan cdpMsg, 1)
	d.pend[id] = ch
	d.mu.Unlock()
	req := map[string]any{"id": id, "method": method}
	if params != nil {
		req["params"] = params
	}
	if sid != "" {
		req["sessionId"] = sid
	}
	b, _ := json.Marshal(req)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := d.c.Write(ctx, websocket.MessageText, b); err != nil {
		return nil, err
	}
	select {
	case m := <-ch:
		if m.Error != nil {
			return nil, fmt.Errorf("%s: %s", method, m.Error.Message)
		}
		return m.Result, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("%s: 超时", method)
	}
}
