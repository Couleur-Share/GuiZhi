package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// 一次采集任务：新建 target → 在导航前启用 Fetch 拦截 → 导航 → 取值 → 关闭。
// 拦截规则（对应产品的 SSRF 防护）：只放行允许的主机；其余请求在发出前失败。
// autoAttach 打开时，弹窗与跨站 iframe 会在“暂停等待调试器”状态下被接管并套用同样的规则。

type capOpts struct {
	navTimeout  time.Duration
	evalTimeout time.Duration
	allow       func(host string) bool
	autoAttach  bool
	afterLoad   func(t *task) // 页面加载后的额外动作（如触发弹窗）
	settle      time.Duration
	blockOpen   bool   // 在每个新文档里把 window.open 换成空函数（只挡 window.open，挡不住 a/form 的 target）
	reuse       string // 不新建 target，直接用这个（采集进程自带的 MyGo 窗口），结束时不关闭
	sandbox     bool   // 给文档响应加 CSP sandbox（不含 allow-popups），由浏览器本身禁止一切弹窗
}

type taskResult struct {
	id        int
	ok        bool
	phase     string // 失败发生的阶段
	err       string
	dur       time.Duration
	closeDur  time.Duration
	title     string
	htmlLen   int
	textLen   int
	blocked   int
	popups    int
	dialogs   int
	crashed   bool
	navErr    string
	unhealthy bool // 窗口需要换掉（卡死、崩溃、加载超时）
}

func (t *task) fetchPatterns() []map[string]any {
	ps := []map[string]any{{"urlPattern": "*", "requestStage": "Request"}}
	if t.o.sandbox {
		ps = append(ps, map[string]any{"urlPattern": "*", "resourceType": "Document", "requestStage": "Response"})
	}
	return ps
}

type task struct {
	b        *browser
	d        *cdp
	id       int
	o        capOpts
	main     string
	targetID string
	loaded   chan struct{}
	mu       sync.Mutex
	blocked  []string
	popups   []string // 弹窗 target
	dialogs  int
	crashed  bool
	subs     []string
	loads    map[string]bool // 已收到 load 生命周期事件的 loaderId
	loadSig  chan struct{}
}

var taskSeq atomic.Int64

func allowLoopbackHosts(hosts ...string) func(string) bool {
	m := map[string]bool{}
	for _, h := range hosts {
		m[h] = true
	}
	return func(h string) bool { return m[h] }
}

func (t *task) onEvent(m cdpMsg) {
	switch m.Method {
	case "Page.lifecycleEvent":
		// 用 loaderId 对应到本次导航：复用的窗口可能带着上一个文档的 load 事件，不能只看“收到了 load”。
		var le struct {
			Name     string `json:"name"`
			LoaderID string `json:"loaderId"`
		}
		json.Unmarshal(m.Params, &le)
		if le.Name == "load" && m.SessionID == t.main {
			t.mu.Lock()
			t.loads[le.LoaderID] = true
			t.mu.Unlock()
			select {
			case t.loadSig <- struct{}{}:
			default:
			}
		}
	case "Page.javascriptDialogOpening":
		t.mu.Lock()
		t.dialogs++
		t.mu.Unlock()
		var dp struct {
			Type string `json:"type"`
		}
		json.Unmarshal(m.Params, &dp)
		// beforeunload 必须接受，否则复用窗口时导航回空白页会被它拦住；其余弹窗一律取消。
		go t.d.call(m.SessionID, "Page.handleJavaScriptDialog", map[string]any{"accept": dp.Type == "beforeunload"})
	case "Inspector.targetCrashed":
		t.mu.Lock()
		t.crashed = true
		t.mu.Unlock()
	case "Fetch.requestPaused":
		go t.decide(m)
	case "Target.attachedToTarget":
		go t.adopt(m)
	}
}

func (t *task) decide(m cdpMsg) {
	var e struct {
		RequestID       string `json:"requestId"`
		ResponseStatus  *int   `json:"responseStatusCode"`
		ResponseHeaders []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"responseHeaders"`
		Request struct {
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"request"`
	}
	json.Unmarshal(m.Params, &e)
	if os.Getenv("CAPDEBUG") != "" {
		fmt.Println("DEBUG paused:", e.Request.URL, e.ResponseStatus != nil)
	}
	if e.ResponseStatus != nil {
		// 响应阶段（仅文档）：非重定向的响应加 CSP sandbox；重定向原样放行（下一跳会重新经过请求阶段）。
		if *e.ResponseStatus >= 300 && *e.ResponseStatus < 400 {
			t.d.call(m.SessionID, "Fetch.continueResponse", map[string]any{"requestId": e.RequestID})
			return
		}
		csp := "sandbox allow-scripts allow-same-origin allow-forms"
		if v := os.Getenv("CAPCSP"); v != "" {
			csp = v
		}
		hs := []map[string]string{{"name": "Content-Security-Policy", "value": csp}}
		for _, h := range e.ResponseHeaders {
			hs = append(hs, map[string]string{"name": h.Name, "value": h.Value})
		}
		if _, err := t.d.call(m.SessionID, "Fetch.continueResponse", map[string]any{"requestId": e.RequestID, "responseCode": *e.ResponseStatus, "responseHeaders": hs}); err != nil {
			fmt.Println("DEBUG continueResponse:", err, e.Request.URL)
		}
		return
	}
	u, err := url.Parse(e.Request.URL)
	host := ""
	if err == nil {
		host = u.Host
		if i := strings.LastIndex(host, ":"); i > 0 && !strings.Contains(host[i:], "]") {
			host = host[:i]
		}
	}
	scheme := ""
	if u != nil {
		scheme = u.Scheme
	}
	if scheme != "http" && scheme != "https" && scheme != "data" && scheme != "blob" && scheme != "about" || (scheme == "http" || scheme == "https") && !t.o.allow(host) {
		t.mu.Lock()
		t.blocked = append(t.blocked, e.Request.URL)
		t.mu.Unlock()
		t.d.call(m.SessionID, "Fetch.failRequest", map[string]any{"requestId": e.RequestID, "errorReason": "BlockedByClient"})
		return
	}
	hs := []map[string]string{{"name": "X-Task", "value": strconv.Itoa(t.id)}}
	for k, v := range e.Request.Headers {
		if !strings.EqualFold(k, "X-Task") {
			hs = append(hs, map[string]string{"name": k, "value": v})
		}
	}
	t.d.call(m.SessionID, "Fetch.continueRequest", map[string]any{"requestId": e.RequestID, "headers": hs})
}

// adopt 接管由页面派生的 target（弹窗、跨站 iframe、worker）：先启用拦截，再放行。
func (t *task) adopt(m cdpMsg) {
	var e struct {
		SessionID          string `json:"sessionId"`
		WaitingForDebugger bool   `json:"waitingForDebugger"`
		TargetInfo         struct {
			TargetID string `json:"targetId"`
			Type     string `json:"type"`
		} `json:"targetInfo"`
	}
	json.Unmarshal(m.Params, &e)
	sid := e.SessionID
	if e.TargetInfo.Type == "page" {
		t.b.tasks.Store(e.TargetInfo.TargetID, t) // 弹窗再弹窗也归同一个任务
	}
	t.d.setSession(sid, t.onEvent)
	t.mu.Lock()
	t.subs = append(t.subs, sid)
	if e.TargetInfo.Type == "page" {
		t.popups = append(t.popups, e.TargetInfo.TargetID)
	}
	t.mu.Unlock()
	t.d.call(sid, "Fetch.enable", map[string]any{"patterns": t.fetchPatterns()})
	t.d.call(sid, "Target.setAutoAttach", map[string]any{"autoAttach": true, "waitForDebuggerOnStart": true, "flatten": true})
	if e.WaitingForDebugger {
		t.d.call(sid, "Runtime.runIfWaitingForDebugger", nil)
	}
}

func (t *task) eval(expr string, timeout time.Duration) (json.RawMessage, error) {
	return t.evalG(expr, timeout, true)
}

// evalG 的 gesture 决定脚本是否带“用户手势”：没有手势时，Chromium 的弹窗拦截会挡住 window.open。
func (t *task) evalG(expr string, timeout time.Duration, gesture bool) (json.RawMessage, error) {
	res, err := t.d.callT(t.main, "Runtime.evaluate", map[string]any{"expression": expr, "returnByValue": true, "userGesture": gesture}, timeout)
	if err != nil {
		return nil, err
	}
	var r struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct{ Text string } `json:"exceptionDetails"`
	}
	json.Unmarshal(res, &r)
	if r.ExceptionDetails != nil {
		return nil, fmt.Errorf("脚本异常：%s", r.ExceptionDetails.Text)
	}
	return r.Result.Value, nil
}

func (b *browser) capture(u string, id int, o capOpts) (res taskResult) {
	res.id = id
	t0 := time.Now()
	defer func() { res.dur = time.Since(t0) }()
	if o.navTimeout == 0 {
		o.navTimeout = 15 * time.Second
	}
	if o.evalTimeout == 0 {
		o.evalTimeout = 8 * time.Second
	}
	t := &task{b: b, d: b.d, id: id, o: o, loaded: make(chan struct{}, 2), loads: map[string]bool{}, loadSig: make(chan struct{}, 1)}
	fail := func(phase string, err error) taskResult {
		res.phase, res.err = phase, err.Error()
		res.unhealthy = !(phase == "navigate" && res.navErr != "") || errors.Is(err, errClosed)
		return res
	}
	var r json.RawMessage
	var err error
	if o.reuse != "" {
		t.targetID = o.reuse
	} else {
		r, err = b.d.call("", "Target.createTarget", map[string]any{"url": "about:blank"})
		if err != nil {
			return fail("createTarget", err)
		}
		var ct struct {
			TargetID string `json:"targetId"`
		}
		json.Unmarshal(r, &ct)
		t.targetID = ct.TargetID
	}
	b.tasks.Store(t.targetID, t)
	defer func() {
		// 关闭：本页、弹窗；记录耗时，并确认 target 真的消失。
		c0 := time.Now()
		ids := append([]string{t.targetID}, t.popups...)
		for _, tid := range ids {
			b.tasks.Delete(tid)
		}
		for i, tid := range ids {
			if i == 0 && o.reuse != "" {
				if res.unhealthy {
					continue // 窗口由池整个换掉，不再往卡死的页面发命令
				}
				b.d.call(t.main, "Fetch.disable", nil) // 否则这个窗口里残留的拦截没人处理，下次导航会挂住
				b.d.call(t.main, "Target.setAutoAttach", map[string]any{"autoAttach": false, "flatten": true})
				b.d.call(t.main, "Page.navigate", map[string]any{"url": "about:blank"})
				continue
			}
			b.d.call("", "Target.closeTarget", map[string]any{"targetId": tid})
		}
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			ts, err := b.targets()
			if err != nil {
				break
			}
			left := false
			for _, x := range ts {
				for _, tid := range ids[map[bool]int{true: 1, false: 0}[o.reuse != ""]:] {
					if x.TargetID == tid {
						left = true
					}
				}
			}
			if !left {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		res.closeDur = time.Since(c0)
		t.mu.Lock()
		res.blocked, res.popups, res.dialogs, res.crashed = len(t.blocked), len(t.popups), t.dialogs, t.crashed
		for _, sid := range t.subs {
			b.d.setSession(sid, nil)
		}
		if t.main != "" {
			b.d.setSession(t.main, nil)
		}
		t.mu.Unlock()
	}()
	r, err = b.d.call("", "Target.attachToTarget", map[string]any{"targetId": t.targetID, "flatten": true})
	if err != nil {
		return fail("attach", err)
	}
	var at struct {
		SessionID string `json:"sessionId"`
	}
	json.Unmarshal(r, &at)
	t.main = at.SessionID
	b.d.setSession(t.main, t.onEvent) // 清理阶段还要处理 beforeunload 弹窗，所以在清理的最后才摘掉
	for _, c := range []struct {
		m string
		p any
	}{
		{"Page.enable", nil},
		{"Runtime.enable", nil},
		{"Page.setLifecycleEventsEnabled", map[string]any{"enabled": true}},
		{"Fetch.enable", map[string]any{"patterns": t.fetchPatterns()}},
	} {
		if _, err := b.d.call(t.main, c.m, c.p); err != nil {
			return fail(c.m, err)
		}
	}
	if o.autoAttach {
		if _, err := b.d.call(t.main, "Target.setAutoAttach", map[string]any{"autoAttach": true, "waitForDebuggerOnStart": true, "flatten": true}); err != nil {
			return fail("setAutoAttach", err)
		}
	}
	if o.blockOpen {
		if _, err := b.d.call(t.main, "Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": "window.open=function(){return null}"}); err != nil {
			return fail("addScript", err)
		}
	}
	nr, err := b.d.callT(t.main, "Page.navigate", map[string]any{"url": u}, o.navTimeout)
	if err != nil {
		return fail("navigate", err)
	}
	var nv struct {
		ErrorText string `json:"errorText"`
		LoaderID  string `json:"loaderId"`
	}
	json.Unmarshal(nr, &nv)
	if nv.ErrorText != "" {
		res.navErr = nv.ErrorText
		return fail("navigate", fmt.Errorf("%s", nv.ErrorText))
	}
	loadDeadline := time.After(o.navTimeout)
	for {
		t.mu.Lock()
		got := nv.LoaderID != "" && t.loads[nv.LoaderID]
		t.mu.Unlock()
		if got {
			break
		}
		select {
		case <-t.loadSig:
		case <-b.d.closed:
			return fail("load", errClosed)
		case <-loadDeadline:
			b.d.call(t.main, "Page.stopLoading", nil)
			return fail("load", fmt.Errorf("加载超时（%s）", o.navTimeout))
		}
	}
	if o.afterLoad != nil {
		o.afterLoad(t)
	}
	if o.settle > 0 {
		time.Sleep(o.settle)
	}
	v, err := t.eval(`JSON.stringify({title:document.title,html:document.documentElement.outerHTML.length,text:(document.body&&document.body.innerText||'').length})`, o.evalTimeout)
	if err != nil {
		return fail("evaluate", err)
	}
	var s string
	json.Unmarshal(v, &s)
	var out struct {
		Title string
		Html  int
		Text  int
	}
	json.Unmarshal([]byte(s), &out)
	res.title, res.htmlLen, res.textLen = out.Title, out.Html, out.Text
	res.ok = true
	return res
}
