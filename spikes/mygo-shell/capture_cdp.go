package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// 最小 CDP 客户端（来自 mygo-capstress 压测原型）：按 sessionId 分发事件；连接断开时立即让所有等待中的调用失败，\n// 采集进程崩溃能被立刻发现，而不是等超时。

type cdpMsg struct {
	ID        int                       `json:"id"`
	Method    string                    `json:"method"`
	Params    json.RawMessage           `json:"params"`
	Result    json.RawMessage           `json:"result"`
	Error     *struct{ Message string } `json:"error"`
	SessionID string                    `json:"sessionId"`
}

type cdpConn struct {
	c      *websocket.Conn
	mu     sync.Mutex
	nextID int
	pend   map[int]chan cdpMsg
	sess   map[string]func(cdpMsg)
	global func(cdpMsg)
	closed chan struct{}
	closeT time.Time
}

var errClosed = errors.New("CDP 连接已断开")

func dialCDP(ctx context.Context, url string) (*cdpConn, error) {
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return nil, err
	}
	c.SetReadLimit(256 << 20)
	d := &cdpConn{c: c, pend: map[int]chan cdpMsg{}, sess: map[string]func(cdpMsg){}, closed: make(chan struct{})}
	go func() {
		for {
			_, b, err := c.Read(context.Background())
			if err != nil {
				d.mu.Lock()
				d.closeT = time.Now()
				close(d.closed)
				for id, ch := range d.pend {
					delete(d.pend, id)
					ch <- cdpMsg{Error: &struct{ Message string }{errClosed.Error()}}
				}
				d.mu.Unlock()
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
			d.mu.Lock()
			h := d.sess[m.SessionID]
			g := d.global
			d.mu.Unlock()
			if h != nil {
				h(m)
			} else if g != nil {
				g(m)
			}
		}
	}()
	return d, nil
}

func (d *cdpConn) setSession(sid string, h func(cdpMsg)) {
	d.mu.Lock()
	if h == nil {
		delete(d.sess, sid)
	} else {
		d.sess[sid] = h
	}
	d.mu.Unlock()
}

func (d *cdpConn) call(sid, method string, params any) (json.RawMessage, error) {
	return d.callT(sid, method, params, 10*time.Second)
}

func (d *cdpConn) callT(sid, method string, params any, timeout time.Duration) (json.RawMessage, error) {
	d.mu.Lock()
	select {
	case <-d.closed:
		d.mu.Unlock()
		return nil, errClosed
	default:
	}
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
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
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
		d.mu.Lock()
		delete(d.pend, id)
		d.mu.Unlock()
		return nil, fmt.Errorf("%s: 超时（%s）", method, timeout)
	}
}
