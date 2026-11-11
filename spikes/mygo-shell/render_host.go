package main

// 对应 web-electron-renderer.ts：网页渲染窗口（需要执行脚本才有正文的页面）。
//
// 安全模型与 Electron 版一致：浏览器本身不联网（见 captureHostRules），所有 http(s) 请求在 CDP 的 Fetch 域暂停，
// 交给 safeFetcher（SSRF 校验 + 钉扎 + 代理）取回后用 Fetch.fulfillRequest 应答；跨源子框架不加载；主框架
// 导航越出目录范围即终止；单页请求数 ≤200、响应总量 ≤50 MiB；图片请求以 1×1 透明 GIF 应答（惰性图片）；
// 网页的 Worker 由响应头 CSP worker-src 'none' 禁止；下载、弹窗一律拒绝。
//
// 与 TS 版的差别：资源类型取自 CDP 的 resourceType（TS 只能按 Accept 粗分）；每个任务用池里的隐藏窗口，
// 结束后清空该任务出现过的源的存储与 Cookie；窗口出现任何异常即整个替换。

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	renderMaxRequests = 200
	renderMaxBytes    = 50 << 20
	renderHTMLLimit   = 10 << 20
	renderPoolSize    = 2
	renderNetSlots    = 8
	renderIdleStop    = 90 * time.Second
)

// 经典的 43 字节 GIF89a：1×1、透明（对应 web-inert-resources.ts）。
var renderTransparentGIF, _ = base64.StdEncoding.DecodeString("R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7")

type renderHost struct {
	fetcher func() *safeFetcher
	exe     string // 空取当前可执行文件
	mu      sync.Mutex
	b       *captureBrowser
	active  int
	idle    *time.Timer
	net     chan struct{}
}

func newRenderHost(fetcher func() *safeFetcher) *renderHost {
	return &renderHost{fetcher: fetcher, net: make(chan struct{}, renderNetSlots)}
}

// acquire 取得一个可用窗口：浏览器未启动或已崩溃则（重新）启动。
func (h *renderHost) acquire(ctx context.Context) (*captureBrowser, string, error) {
	h.mu.Lock()
	if h.idle != nil {
		h.idle.Stop()
	}
	if h.b != nil && !h.b.alive() {
		h.b.kill()
		h.b = nil
	}
	if h.b == nil {
		b, err := startCaptureBrowser(ctx, h.exe, renderPoolSize)
		if err != nil {
			h.mu.Unlock()
			return nil, "", newWebCaptureFailure("网页渲染进程启动失败："+err.Error(), "empty")
		}
		h.b = b
	}
	b := h.b
	h.active++
	h.mu.Unlock()
	tid, err := b.pool.acquire(ctx)
	if err != nil {
		h.release(b, "", false)
		if ctx.Err() != nil {
			return nil, "", webAbortFailure(ctx)
		}
		return nil, "", newWebCaptureFailure("网页渲染进程已断开："+err.Error(), "empty")
	}
	return b, tid, nil
}

func (h *renderHost) release(b *captureBrowser, tid string, unhealthy bool) {
	if tid != "" {
		b.pool.release(tid, unhealthy)
	}
	h.mu.Lock()
	h.active--
	if h.active == 0 && h.b == b {
		h.idle = time.AfterFunc(renderIdleStop, func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			if h.active == 0 && h.b == b {
				h.b = nil
				go b.stop()
			}
		})
	}
	h.mu.Unlock()
}

// Close 关闭采集浏览器（应用退出时）。
func (h *renderHost) Close() {
	h.mu.Lock()
	b := h.b
	h.b = nil
	if h.idle != nil {
		h.idle.Stop()
	}
	h.mu.Unlock()
	if b != nil {
		b.stop()
	}
}

func (h *renderHost) Render(ctx context.Context, req webCaptureRequest) (*renderedWebPage, error) {
	b, tid, err := h.acquire(ctx)
	if err != nil {
		return nil, err
	}
	t := newRenderTask(h, b, tid, req)
	page, err := t.run(ctx)
	h.release(b, tid, t.unhealthy)
	return page, err
}

// ---- 活动跟踪（对应 web-network-activity.ts）----

// affectsDocument：样式表、图片、文档类请求（子框架、预取）不改动主文档快照，不计入网络静止判定。
func renderAffectsDocument(kind string) bool {
	return kind != "style" && kind != "image" && kind != "document"
}

func renderKind(resourceType string) string {
	switch resourceType {
	case "Document":
		return "document"
	case "Stylesheet":
		return "style"
	case "Image":
		return "image"
	}
	return "other"
}

type renderActivity struct {
	mu     sync.Mutex
	active int
	lastAt time.Time
}

func (a *renderActivity) begin(kind string) func() {
	if !renderAffectsDocument(kind) {
		return func() {}
	}
	a.mu.Lock()
	a.active++
	a.lastAt = time.Now()
	a.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			a.mu.Lock()
			a.active--
			a.lastAt = time.Now()
			a.mu.Unlock()
		})
	}
}

func (a *renderActivity) quiet() time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.active > 0 {
		return 0
	}
	return time.Since(a.lastAt)
}

// ---- 单次渲染任务 ----

type renderResp struct {
	status      int
	contentType string
}

type renderTask struct {
	h        *renderHost
	b        *captureBrowser
	d        *cdpConn
	targetID string
	req      webCaptureRequest
	fetcher  *safeFetcher

	ctx    context.Context
	cancel context.CancelCauseFunc

	sid       string
	mainFrame string
	loadSig   chan struct{}

	mu            sync.Mutex
	topURL        string
	fatal         error
	responses     map[string]renderResp
	navigations   map[string]bool
	hosts         map[string]int
	origins       map[string]bool
	subs          []string
	requests      int
	bytes         int64
	blockedFrames int
	inertStubs    int
	navigating    bool // Page.navigate 发出之后才开始接受 load（启用生命周期事件时会重放空白页的旧事件）
	loaded        bool
	crashed       bool
	unhealthy     bool

	wg  sync.WaitGroup
	act renderActivity
}

func newRenderTask(h *renderHost, b *captureBrowser, targetID string, req webCaptureRequest) *renderTask {
	f := h.fetcher()
	if f == nil {
		f = defaultSafeFetcher
	}
	return &renderTask{
		h: h, b: b, d: b.d, targetID: targetID, req: req, fetcher: f,
		loadSig:     make(chan struct{}, 1),
		responses:   map[string]renderResp{},
		navigations: map[string]bool{req.URL: true},
		hosts:       map[string]int{},
		origins:     map[string]bool{},
		act:         renderActivity{lastAt: time.Now()},
	}
}

func (t *renderTask) setFatal(err error) {
	t.mu.Lock()
	if t.fatal == nil {
		t.fatal = err
	}
	t.mu.Unlock()
	t.cancel(err)
}

func (t *renderTask) fatalErr() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.fatal
}

// call 带取消的 CDP 调用。
func (t *renderTask) call(sid, method string, params any, timeout time.Duration) (json.RawMessage, error) {
	type result struct {
		r   json.RawMessage
		err error
	}
	ch := make(chan result, 1)
	go func() {
		r, err := t.d.callT(sid, method, params, timeout)
		ch <- result{r, err}
	}()
	select {
	case r := <-ch:
		return r.r, r.err
	case <-t.ctx.Done():
		return nil, t.ctx.Err()
	}
}

// eval 在主框架执行表达式并取值；执行上下文因导航被销毁时稍等重试（对应 executeJavaScript 等新文档就绪）。
func (t *renderTask) eval(expr string) (json.RawMessage, error) {
	var lastErr error
	for attempt := 0; attempt < 6; attempt++ {
		res, err := t.call(t.sid, "Runtime.evaluate", map[string]any{"expression": expr, "returnByValue": true}, 60*time.Second)
		if err == nil {
			var r struct {
				Result           struct{ Value json.RawMessage } `json:"result"`
				ExceptionDetails *struct{ Text string }          `json:"exceptionDetails"`
			}
			json.Unmarshal(res, &r)
			if r.ExceptionDetails != nil {
				return nil, fmt.Errorf("页面脚本异常：%s", r.ExceptionDetails.Text)
			}
			return r.Result.Value, nil
		}
		lastErr = err
		if t.ctx.Err() != nil || !(strings.Contains(err.Error(), "context") || strings.Contains(err.Error(), "Context")) {
			break
		}
		if sleepCtx(t.ctx, 50*time.Millisecond) != nil {
			break
		}
	}
	return nil, lastErr
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ---- 事件 ----

func (t *renderTask) onEvent(m cdpMsg) {
	switch m.Method {
	case "Page.lifecycleEvent":
		var le struct {
			Name    string `json:"name"`
			FrameID string `json:"frameId"`
		}
		json.Unmarshal(m.Params, &le)
		if le.Name == "load" && m.SessionID == t.sid && le.FrameID == t.mainFrame {
			t.mu.Lock()
			if t.navigating {
				t.loaded = true
			}
			t.mu.Unlock()
			select {
			case t.loadSig <- struct{}{}:
			default:
			}
		}
	case "Page.frameNavigated":
		var fn struct {
			Frame struct {
				ParentID string `json:"parentId"`
				URL      string `json:"url"`
			} `json:"frame"`
		}
		json.Unmarshal(m.Params, &fn)
		if fn.Frame.ParentID == "" && m.SessionID == t.sid {
			t.mu.Lock()
			t.topURL = fn.Frame.URL
			t.mu.Unlock()
		}
	case "Page.javascriptDialogOpening":
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
		t.setFatal(errors.New("网页渲染进程崩溃"))
	case "Fetch.requestPaused":
		t.wg.Add(1)
		go func() {
			defer t.wg.Done()
			t.onPaused(m)
		}()
	case "Target.attachedToTarget":
		go t.adopt(m)
	}
}

// adopt 接管由页面派生的 target：弹窗直接关闭；其余（跨站子框架、worker）先套上同样的拦截再放行。
func (t *renderTask) adopt(m cdpMsg) {
	var e struct {
		SessionID          string `json:"sessionId"`
		WaitingForDebugger bool   `json:"waitingForDebugger"`
		TargetInfo         struct {
			TargetID string `json:"targetId"`
			Type     string `json:"type"`
		} `json:"targetInfo"`
	}
	json.Unmarshal(m.Params, &e)
	if e.TargetInfo.Type == "page" {
		t.d.call("", "Target.closeTarget", map[string]any{"targetId": e.TargetInfo.TargetID})
		return
	}
	t.d.setSession(e.SessionID, t.onEvent)
	t.mu.Lock()
	t.subs = append(t.subs, e.SessionID)
	t.mu.Unlock()
	t.d.call(e.SessionID, "Fetch.enable", map[string]any{"patterns": []map[string]any{{"urlPattern": "*", "requestStage": "Request"}}})
	t.d.call(e.SessionID, "Target.setAutoAttach", map[string]any{"autoAttach": true, "waitForDebuggerOnStart": true, "flatten": true})
	if e.WaitingForDebugger {
		t.d.call(e.SessionID, "Runtime.runIfWaitingForDebugger", nil)
	}
}

// shouldBlockSubframe 对应 web-subframe-policy.ts：不加载跨源子框架，同源照常；无法判断一律不拦截。
func shouldBlockSubframe(frameURL, topURL string) bool {
	if os.Getenv("GUIZHI_WEB_SUBFRAMES") == "allow" {
		return false
	}
	f, err1 := url.Parse(frameURL)
	top, err2 := url.Parse(topURL)
	if err1 != nil || err2 != nil {
		return false
	}
	isHTTP := func(u *url.URL) bool { return u.Scheme == "http" || u.Scheme == "https" }
	if !isHTTP(f) || !isHTTP(top) {
		return false
	}
	return f.Scheme != top.Scheme || strings.ToLower(f.Host) != strings.ToLower(top.Host)
}

type pausedRequest struct {
	RequestID    string `json:"requestId"`
	FrameID      string `json:"frameId"`
	ResourceType string `json:"resourceType"`
	Request      struct {
		URL             string            `json:"url"`
		Method          string            `json:"method"`
		Headers         map[string]string `json:"headers"`
		PostData        string            `json:"postData"`
		PostDataEntries []struct {
			Bytes string `json:"bytes"`
		} `json:"postDataEntries"`
	} `json:"request"`
}

func summarizeRenderHosts(hosts map[string]int) string {
	type kv struct {
		k string
		v int
	}
	var list []kv
	for k, v := range hosts {
		list = append(list, kv{k, v})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].v > list[j].v || (list[i].v == list[j].v && list[i].k < list[j].k) })
	if len(list) > 3 {
		list = list[:3]
	}
	parts := make([]string, len(list))
	for i, e := range list {
		parts[i] = fmt.Sprintf("%s ×%d", e.k, e.v)
	}
	return strings.Join(parts, "、")
}

func (t *renderTask) fail(sid string, p pausedRequest, reason string) {
	t.d.call(sid, "Fetch.failRequest", map[string]any{"requestId": p.RequestID, "errorReason": reason})
}

func (t *renderTask) onPaused(m cdpMsg) {
	sid := m.SessionID
	var p pausedRequest
	json.Unmarshal(m.Params, &p)
	u, err := url.Parse(p.Request.URL)
	if err != nil {
		t.fail(sid, p, "BlockedByClient")
		return
	}
	switch u.Scheme {
	case "data", "blob", "about":
		t.d.call(sid, "Fetch.continueRequest", map[string]any{"requestId": p.RequestID})
		return
	case "http", "https":
	default:
		// 非 http(s)（含 ftp、file、chrome-extension 等）一律取消。
		t.fail(sid, p, "BlockedByClient")
		return
	}
	t.mu.Lock()
	t.origins[u.Scheme+"://"+u.Host] = true
	t.mu.Unlock()
	isMain := p.ResourceType == "Document" && p.FrameID == t.mainFrame && sid == t.sid
	if p.ResourceType == "Document" {
		if !isMain {
			// 子框架：取消跨源的，理由见 shouldBlockSubframe；这不是错误，采集照常进行。
			t.mu.Lock()
			top := t.topURL
			t.mu.Unlock()
			if shouldBlockSubframe(p.Request.URL, top) {
				t.mu.Lock()
				t.blockedFrames++
				t.mu.Unlock()
				t.fail(sid, p, "BlockedByClient")
				return
			}
		} else {
			// 主框架：越界即取消并终止采集；每一跳（含服务端重定向）都经过这里。
			if t.req.Scope != nil && !inWebScope(p.Request.URL, *t.req.Scope) {
				t.fail(sid, p, "BlockedByClient")
				t.setFatal(newWebCaptureFailure("导航超出允许范围", "security"))
				return
			}
			t.mu.Lock()
			t.navigations[p.Request.URL] = true
			t.mu.Unlock()
		}
	}
	// 惰性图片：不出网，也不占请求数预算与网络槽位。
	if p.ResourceType == "Image" && p.Request.Method == "GET" && os.Getenv("GUIZHI_WEB_INERT_RESOURCES") != "load" {
		t.mu.Lock()
		t.inertStubs++
		t.mu.Unlock()
		hs := []map[string]string{
			{"name": "content-type", "value": "image/gif"},
			{"name": "cache-control", "value": "no-store"},
			{"name": "cross-origin-resource-policy", "value": "cross-origin"},
		}
		for k, v := range p.Request.Headers {
			if strings.EqualFold(k, "origin") && v != "" {
				hs = append(hs, map[string]string{"name": "access-control-allow-origin", "value": v},
					map[string]string{"name": "access-control-allow-credentials", "value": "true"},
					map[string]string{"name": "vary", "value": "Origin"})
			}
		}
		t.d.call(sid, "Fetch.fulfillRequest", map[string]any{"requestId": p.RequestID, "responseCode": 200, "responseHeaders": hs, "body": base64.StdEncoding.EncodeToString(renderTransparentGIF)})
		return
	}
	// 请求数预算：入队前计数，避免页面一次发起大量请求时先堆满等待队列。
	t.mu.Lock()
	t.hosts[u.Host]++
	t.requests++
	over := t.requests > renderMaxRequests
	summary := ""
	if over {
		summary = summarizeRenderHosts(t.hosts)
	}
	t.mu.Unlock()
	if over {
		t.fail(sid, p, "BlockedByClient")
		t.setFatal(fmt.Errorf("页面网络预算已用尽（超过 %d 个请求，主要来源：%s）", renderMaxRequests, summary))
		return
	}
	ended := t.act.begin(renderKind(p.ResourceType))
	defer ended()
	resp, err := t.egress(p)
	if err != nil {
		code := webCaptureErrorOf(err).Code
		// 已取消/超时造成的出口失败不是页面自身的错误，由调用方的取消原因统一报告。
		if t.ctx.Err() == nil && (isMain || code == "security" || code == "incomplete") {
			t.setFatal(err)
		}
		t.fail(sid, p, "Failed")
		return
	}
	t.mu.Lock()
	t.bytes += int64(len(resp.Body))
	tooBig := t.bytes > renderMaxBytes
	if !tooBig {
		t.responses[p.Request.URL] = renderResp{status: resp.Status, contentType: resp.Headers["content-type"]}
	}
	t.mu.Unlock()
	if tooBig {
		t.setFatal(errors.New("页面网络响应总量超过 50 MiB"))
		t.fail(sid, p, "Failed")
		return
	}
	var hs []map[string]string
	for k, v := range resp.Headers {
		for _, part := range strings.Split(v, "\n") {
			hs = append(hs, map[string]string{"name": k, "value": part})
		}
	}
	// 禁止网页创建 Service Worker / 其他 Worker 绕过页面请求的生命周期。
	hs = append(hs, map[string]string{"name": "content-security-policy", "value": "worker-src 'none'"})
	body := ""
	if resp.Status != 204 && resp.Status != 205 && resp.Status != 304 {
		body = base64.StdEncoding.EncodeToString(resp.Body)
	}
	t.d.call(sid, "Fetch.fulfillRequest", map[string]any{"requestId": p.RequestID, "responseCode": resp.Status, "responseHeaders": hs, "body": body})
}

// egress 经全局网络槽位走安全出口取回响应。
func (t *renderTask) egress(p pausedRequest) (webHop, error) {
	select {
	case t.h.net <- struct{}{}:
		defer func() { <-t.h.net }()
	case <-t.ctx.Done():
		return webHop{}, webAbortFailure(t.ctx)
	}
	var body []byte
	if len(p.Request.PostDataEntries) > 0 {
		for _, e := range p.Request.PostDataEntries {
			b, err := base64.StdEncoding.DecodeString(e.Bytes)
			if err != nil {
				return webHop{}, errors.New("网页请求体无法读取")
			}
			body = append(body, b...)
			if len(body) > webSubRequestBodyLimit {
				return webHop{}, errors.New("网页请求体超过 1 MiB")
			}
		}
	} else if p.Request.PostData != "" {
		body = []byte(p.Request.PostData)
		if len(body) > webSubRequestBodyLimit {
			return webHop{}, errors.New("网页请求体超过 1 MiB")
		}
	}
	return t.fetcher.webSubRequest(t.ctx, webSubRequest{URL: p.Request.URL, Method: p.Request.Method, Headers: p.Request.Headers, Body: body})
}

// ---- 沉降环境 ----

type renderSettleEnv struct{ t *renderTask }

func (e renderSettleEnv) now() time.Time                                   { return time.Now() }
func (e renderSettleEnv) pause(ctx context.Context, d time.Duration) error { return sleepCtx(ctx, d) }
func (e renderSettleEnv) networkQuiet() time.Duration                      { return e.t.act.quiet() }
func (e renderSettleEnv) scroll(ctx context.Context) error {
	_, err := e.t.eval("window.scrollBy(0, Math.min(window.innerHeight, 1000))")
	return err
}
func (e renderSettleEnv) install(ctx context.Context) (bool, error) {
	v, err := e.t.eval(renderScriptInstallProbe)
	if err != nil {
		return false, err
	}
	return string(v) == "true", nil
}
func (e renderSettleEnv) sample(ctx context.Context) (*settleSample, error) {
	v, err := e.t.eval(renderScriptReadProbe)
	if err != nil {
		return nil, err
	}
	if string(v) == "null" || len(v) == 0 {
		return nil, nil
	}
	var s settleSample
	if json.Unmarshal(v, &s) != nil {
		return nil, errors.New("页面探针返回了无法识别的数据")
	}
	return &s, nil
}

// ---- 主流程 ----

func (t *renderTask) run(parent context.Context) (page *renderedWebPage, err error) {
	t.ctx, t.cancel = context.WithCancelCause(parent)
	defer t.cancel(nil)
	defer func() {
		// 优先报告先发生的致命错误（越界导航、预算用尽、出口拒绝），其次是取消/超时。
		if fe := t.fatalErr(); fe != nil {
			page, err = nil, fe
		} else if err != nil && parent.Err() != nil {
			page, err = nil, webAbortFailure(parent)
		}
		if err != nil {
			t.unhealthy = true
		}
	}()
	defer t.cleanup()

	t.b.tasks.Store(t.targetID, t)
	r, err := t.d.call("", "Target.attachToTarget", map[string]any{"targetId": t.targetID, "flatten": true})
	if err != nil {
		t.unhealthy = true
		return nil, fmt.Errorf("连接渲染窗口失败：%w", err)
	}
	var at struct {
		SessionID string `json:"sessionId"`
	}
	json.Unmarshal(r, &at)
	t.sid = at.SessionID
	t.d.setSession(t.sid, t.onEvent)
	for _, c := range []struct {
		method string
		params any
	}{
		{"Page.enable", nil},
		{"Runtime.enable", nil},
		{"Page.setLifecycleEventsEnabled", map[string]any{"enabled": true}},
		{"Emulation.setUserAgentOverride", map[string]any{"userAgent": webUserAgent()}},
		{"Fetch.enable", map[string]any{"patterns": []map[string]any{{"urlPattern": "*", "requestStage": "Request"}}}},
		{"Target.setAutoAttach", map[string]any{"autoAttach": true, "waitForDebuggerOnStart": true, "flatten": true}},
	} {
		if _, err := t.call(t.sid, c.method, c.params, 15*time.Second); err != nil {
			t.unhealthy = true
			return nil, fmt.Errorf("初始化渲染窗口失败（%s）：%w", c.method, err)
		}
	}
	tree, err := t.call(t.sid, "Page.getFrameTree", nil, 15*time.Second)
	if err != nil {
		return nil, err
	}
	var ft struct {
		FrameTree struct {
			Frame struct {
				ID string `json:"id"`
			} `json:"frame"`
		} `json:"frameTree"`
	}
	json.Unmarshal(tree, &ft)
	t.mainFrame = ft.FrameTree.Frame.ID

	t.mu.Lock()
	t.navigating = true
	t.mu.Unlock()
	nr, err := t.call(t.sid, "Page.navigate", map[string]any{"url": t.req.URL}, 120*time.Second)
	if err != nil {
		return nil, err
	}
	var nv struct {
		ErrorText string `json:"errorText"`
		LoaderID  string `json:"loaderId"`
	}
	json.Unmarshal(nr, &nv)
	if nv.ErrorText != "" {
		return nil, fmt.Errorf("页面加载失败：%s", nv.ErrorText)
	}
	for {
		t.mu.Lock()
		// 脚本跳转时首个文档来不及触发 load 就被替换，所以不按 loaderId 对应，接受导航开始后任意一次主框架 load。
		got := t.loaded
		t.mu.Unlock()
		if got {
			break
		}
		select {
		case <-t.loadSig:
		case <-t.d.closed:
			t.unhealthy = true
			return nil, errors.New("网页渲染进程已断开")
		case <-t.ctx.Done():
			return nil, t.ctx.Err()
		}
	}
	if _, err := settleRenderedPage(t.ctx, renderSettleEnv{t}, os.Getenv("GUIZHI_WEB_SETTLE") == "fixed"); err != nil {
		return nil, err
	}
	return t.snapshot()
}

func (t *renderTask) snapshot() (*renderedWebPage, error) {
	v, err := t.eval(renderScriptSnapshot)
	if err != nil {
		return nil, err
	}
	var snap struct {
		HTML  string `json:"html"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(v, &snap); err != nil {
		return nil, errors.New("渲染后页面快照无法读取")
	}
	if snap.Error != "" {
		return nil, newWebCaptureFailure(snap.Error, "incomplete")
	}
	if len(snap.HTML) > renderHTMLLimit {
		return nil, errors.New("渲染后 HTML 超过 10 MiB")
	}
	hv, err := t.eval("location.href")
	if err != nil {
		return nil, err
	}
	var finalURL string
	json.Unmarshal(hv, &finalURL)
	if t.req.Scope != nil && !inWebScope(finalURL, *t.req.Scope) {
		return nil, errors.New("最终网址超出目录范围")
	}
	key := finalURL
	if i := strings.Index(key, "#"); i >= 0 {
		key = key[:i]
	}
	t.mu.Lock()
	resp, ok := t.responses[key]
	t.mu.Unlock()
	if !ok {
		return nil, errors.New("未取得网页主文档响应")
	}
	if reWebBinaryType.MatchString(resp.contentType) {
		return nil, newWebCaptureFailure("入口返回非网页内容", "incomplete")
	}
	lv, err := t.eval("Array.from(document.querySelectorAll('a[href]')).slice(0, 2000).map(a => a.href).filter(url => /^https?:/.test(url))")
	if err != nil {
		return nil, err
	}
	var links []string
	json.Unmarshal(lv, &links)
	return &renderedWebPage{HTML: snap.HTML, URL: finalURL, Status: resp.status, Links: links}, nil
}

// cleanup 摘掉拦截、回到空白页并清掉本任务出现过的源的存储与 Cookie；任何一步失败都让窗口被替换。
func (t *renderTask) cleanup() {
	t.cancel(nil)
	t.b.tasks.Delete(t.targetID)
	alive := t.sid != "" && !t.unhealthy && t.d != nil
	done := make(chan struct{})
	go func() { t.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.unhealthy = true
	}
	if alive {
		call := func(method string, params any) {
			if _, err := t.d.callT(t.sid, method, params, 5*time.Second); err != nil {
				t.unhealthy = true
			}
		}
		call("Fetch.disable", nil)
		call("Target.setAutoAttach", map[string]any{"autoAttach": false, "flatten": true})
		call("Page.navigate", map[string]any{"url": "about:blank"})
		call("Network.clearBrowserCookies", nil)
		t.mu.Lock()
		origins := make([]string, 0, len(t.origins))
		for o := range t.origins {
			origins = append(origins, o)
		}
		t.mu.Unlock()
		for _, o := range origins {
			call("Storage.clearDataForOrigin", map[string]any{"origin": o, "storageTypes": "all"})
		}
		call("Network.clearBrowserCache", nil)
	}
	t.mu.Lock()
	if t.crashed {
		t.unhealthy = true
	}
	subs := t.subs
	t.mu.Unlock()
	for _, s := range subs {
		t.d.setSession(s, nil)
	}
	if t.sid != "" {
		t.d.setSession(t.sid, nil)
	}
}
