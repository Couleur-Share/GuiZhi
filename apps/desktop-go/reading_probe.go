package main

// 阅读页运行检查：对应 v3-views.ts 的 probeReadingPage。
// Electron 版用一个隐藏 BrowserWindow 加载真实阅读视图；这里借用渲染进程用的隐藏 WebView2 采集窗口：
// 页面无需任何网络（CSP 已封死，素材全部内联），所以不套 Fetch 拦截，只用 Runtime.addBinding 接收页面消息。
// 判定规则与 TS 相同：6.5 秒内出现故障即返回故障说明；就绪且已过 750 毫秒返回通过；超时返回「阅读交互初始化未完成」。

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

const readingProbeBinding = "gzReading"

func (l *Library) probeReadingPage(rh *renderHost, ctx context.Context, versionJSON string) (string, error) {
	v, err := jsParse(versionJSON)
	if err != nil {
		return "", err
	}
	page, ok := v.(*jsObj)
	if !ok {
		return "", fmt.Errorf("阅读页版本格式无效")
	}
	runnable := false
	if d, ok := page.get("design").(*jsObj); ok {
		for _, s := range themedArr(d.get("scripts")) {
			if themedStr(s, "status") != "failed" {
				runnable = true
			}
		}
	}
	if !runnable {
		return "", nil
	}
	b, tid, err := rh.acquire(ctx)
	if err != nil {
		return "", err
	}
	unhealthy := false
	defer func() { rh.release(b, tid, unhealthy) }()

	r, err := b.d.call("", "Target.attachToTarget", map[string]any{"targetId": tid, "flatten": true})
	if err != nil {
		unhealthy = true
		return "", fmt.Errorf("连接检查窗口失败：%w", err)
	}
	var at struct {
		SessionID string `json:"sessionId"`
	}
	json.Unmarshal(r, &at)
	sid := at.SessionID
	h := &readingViewHost{l: l, views: map[string]*readingView{}}
	e := &readingView{id: themedUUID(), page: page, enabled: true, stop: make(chan struct{})}
	h.views[e.id] = e
	call := func(method string, params any) (json.RawMessage, error) {
		return b.d.callT(sid, method, params, 15*time.Second)
	}
	post := func(command string) {
		expr := fmt.Sprintf(`(()=>{const f=document.getElementById('h');if(f&&f.contentWindow)f.contentWindow.postMessage({gzReadingCommand:%s,viewId:%q},'*')})()`, command, e.id)
		_, _ = call("Runtime.evaluate", map[string]any{"expression": expr})
	}
	h.push = func(ch string, payload any) {
		if ch == chReadingViewCmd {
			if s, ok := jsStringify(jsGet(payload, "command")); ok {
				go post(s)
			}
		}
	}
	defer func() {
		h.stopEntry(e)
		b.d.setSession(sid, nil)
		if _, err := b.d.callT(sid, "Page.navigate", map[string]any{"url": "about:blank"}, 5*time.Second); err != nil {
			unhealthy = true
		}
		_, _ = b.d.callT("", "Target.detachFromTarget", map[string]any{"sessionId": sid}, 5*time.Second)
	}()
	b.d.setSession(sid, func(m cdpMsg) {
		switch m.Method {
		case "Runtime.bindingCalled":
			var p struct{ Name, Payload string }
			json.Unmarshal(m.Params, &p)
			if p.Name != readingProbeBinding {
				return
			}
			var d any
			if json.Unmarshal([]byte(p.Payload), &d) == nil {
				if jv, err := jsParse(p.Payload); err == nil {
					h.receive(e.id, jv)
				}
			}
		case "Inspector.targetCrashed":
			h.fault(e, "阅读页渲染进程崩溃")
		case "Page.javascriptDialogOpening":
			go b.d.call(m.SessionID, "Page.handleJavaScriptDialog", map[string]any{"accept": false})
		}
	})
	for _, c := range []struct {
		method string
		params any
	}{
		{"Page.enable", nil},
		{"Runtime.enable", nil},
		{"Runtime.addBinding", map[string]any{"name": readingProbeBinding}},
	} {
		if _, err := call(c.method, c.params); err != nil {
			unhealthy = true
			return "", fmt.Errorf("初始化检查窗口失败（%s）：%w", c.method, err)
		}
	}
	doc, err := h.document(e)
	if err != nil {
		return err.Error(), nil // 与 TS 一致：创建视图失败就以其 message 作为故障说明
	}
	docJSON, _ := json.Marshal(doc)
	vid, _ := json.Marshal(e.id)
	script := fmt.Sprintf(`(()=>{const v=%s;const f=document.createElement('iframe');f.id='h';f.setAttribute('sandbox','allow-scripts');f.srcdoc=%s;document.body.appendChild(f);
addEventListener('message',e=>{const d=e.data;if(e.source===f.contentWindow&&d&&d.gzReadingView===v&&d.data&&d.data.reading===3)window.%s(JSON.stringify(d.data))});})()`, vid, docJSON, readingProbeBinding)
	if _, err := call("Runtime.evaluate", map[string]any{"expression": script}); err != nil {
		unhealthy = true
		return "", fmt.Errorf("加载阅读页失败：%w", err)
	}
	h.startHeartbeat(e)
	start := time.Now()
	for time.Since(start) < 6500*time.Millisecond {
		h.mu.Lock()
		fault, ready := e.fault, e.ready
		h.mu.Unlock()
		if fault != "" {
			return fault, nil
		}
		if ready && time.Since(start) > 750*time.Millisecond {
			return "", nil
		}
		if err := sleepCtx(ctx, 100*time.Millisecond); err != nil {
			unhealthy = true
			return "", err
		}
	}
	return "阅读交互初始化未完成", nil
}
