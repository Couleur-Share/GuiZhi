package main

// 平台登录采集用的持久浏览器：同一个 exe 的采集子进程 + 固定的 WebView2 用户数据目录（Cookie 与登录态落盘），
// 主进程用 CDP 控制。对应 platform-capture/electron-capture-runtime.ts（每个平台独立 partition = 独立 profile 目录）。
// 与渲染池（render_host.go）的区别：用户数据目录是持久的、不屏蔽域名解析（真实联网，经平台白名单拦截）、
// 登录时窗口可见；一次操作启动一个浏览器，操作结束优雅退出以保证 Cookie 落盘。

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	jsonResponseLimit        = 64
	jsonResponseBytesLimit   = 32 << 20
	singleJSONResponseLimit  = 10 << 20
	platformBrowserStopGrace = 10 * time.Second
)

type platformBrowser struct {
	cmd      *exec.Cmd
	d        *cdpConn
	stdin    io.WriteCloser
	userData string
	targetID string
	version  string
	destroy  chan struct{} // 页面窗口被关闭（用户关掉登录窗口）
	destroyO sync.Once
}

// platformProxyArgs 把应用的网络代理设置转成 Chromium 命令行参数（对应 applyElectronSessionProxy）。
// 代理口令不进命令行（Chromium 也不识别 URL 里的口令），由 Fetch.authRequired 提供。
func platformProxyArgs(s networkProxySettings) string {
	switch s.Mode {
	case "direct":
		return "--no-proxy-server"
	case "system":
		return ""
	}
	raw := buildProxyURL(s)
	if raw == "" {
		return "--no-proxy-server"
	}
	host := s.Host
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	args := fmt.Sprintf(`--proxy-server="%s://%s:%d"`, s.Protocol, host, s.Port)
	if bypass := strings.Trim(bypassSplitRe.ReplaceAllString(s.Bypass, ";"), ";"); bypass != "" {
		args += fmt.Sprintf(` --proxy-bypass-list="%s"`, bypass)
	}
	return args
}

type platformBrowserOptions struct {
	Exe       string // 空取当前可执行文件
	UserData  string
	Window    string // visible | offscreen | ""（隐藏）
	Title     string
	ProxyArgs string
}

func startPlatformBrowser(ctx context.Context, o platformBrowserOptions) (*platformBrowser, error) {
	exe := o.Exe
	if exe == "" {
		var err error
		if exe, err = os.Executable(); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(o.UserData, 0o700); err != nil {
		return nil, err
	}
	cmd := exec.Command(exe)
	args := "--remote-debugging-port=0 --force-webrtc-ip-handling-policy=disable_non_proxied_udp"
	if o.ProxyArgs != "" {
		args += " " + o.ProxyArgs
	}
	cmd.Env = append(os.Environ(),
		captureChildEnv+"="+o.UserData,
		"WEBVIEW2_USER_DATA_FOLDER="+o.UserData,
		"WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS="+args,
		captureWindowEnv+"="+o.Window,
		captureTitleEnv+"="+o.Title)
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	lineCh := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		first := true
		for sc.Scan() {
			if first {
				first = false
				lineCh <- strings.TrimSpace(sc.Text())
			}
		}
		if first {
			lineCh <- "ERR 浏览器进程提前退出"
		}
	}()
	fail := func(err error) (*platformBrowser, error) {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, err
	}
	var line string
	select {
	case line = <-lineCh:
	case <-ctx.Done():
		return fail(ctx.Err())
	case <-time.After(40 * time.Second):
		return fail(errors.New("浏览器进程启动超时"))
	}
	f := strings.Fields(line)
	if len(f) != 3 || f[0] != "READY" {
		return fail(fmt.Errorf("浏览器进程启动失败：%s", line))
	}
	d, err := dialCDP(ctx, "ws://127.0.0.1:"+f[1]+f[2])
	if err != nil {
		return fail(err)
	}
	b := &platformBrowser{cmd: cmd, d: d, stdin: stdin, userData: o.UserData, destroy: make(chan struct{})}
	kill := func(err error) (*platformBrowser, error) {
		b.kill()
		return nil, err
	}
	d.global = b.onGlobal
	if _, err := d.call("", "Target.setDiscoverTargets", map[string]any{"discover": true}); err != nil {
		return kill(err)
	}
	// 网页不得触发下载
	if _, err := d.call("", "Browser.setDownloadBehavior", map[string]any{"behavior": "deny"}); err != nil {
		return kill(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for b.targetID == "" && time.Now().Before(deadline) {
		res, err := d.call("", "Target.getTargets", nil)
		if err != nil {
			return kill(err)
		}
		var ts struct {
			TargetInfos []captureTarget `json:"targetInfos"`
		}
		_ = json.Unmarshal(res, &ts)
		for _, t := range ts.TargetInfos {
			if t.Type == "page" {
				b.targetID = t.TargetID
				break
			}
		}
		if b.targetID == "" {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if b.targetID == "" {
		return kill(errors.New("浏览器没有出现页面窗口"))
	}
	if res, err := d.call("", "Browser.getVersion", nil); err == nil {
		var v struct {
			Product string `json:"product"`
		}
		_ = json.Unmarshal(res, &v)
		b.version = strings.TrimPrefix(v.Product, "Edg/")
		b.version = strings.TrimPrefix(b.version, "Chrome/")
	}
	return b, nil
}

func (b *platformBrowser) onGlobal(m cdpMsg) {
	if m.Method != "Target.targetDestroyed" {
		return
	}
	var e struct {
		TargetID string `json:"targetId"`
	}
	_ = json.Unmarshal(m.Params, &e)
	if e.TargetID == b.targetID {
		b.destroyO.Do(func() { close(b.destroy) })
	}
}

func (b *platformBrowser) alive() bool {
	select {
	case <-b.d.closed:
		return false
	case <-b.destroy:
		return false
	default:
		return true
	}
}

// stop 优雅退出（关闭标准输入 → 子进程调用 App.Quit），让 Cookie 等落盘；超时才强杀。
func (b *platformBrowser) stop() {
	_ = b.stdin.Close()
	done := make(chan struct{})
	go func() { _ = b.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(platformBrowserStopGrace):
		_ = b.cmd.Process.Kill()
		<-done
	}
}

func (b *platformBrowser) kill() {
	_ = b.cmd.Process.Kill()
	_ = b.cmd.Wait()
}

// ---------- 页面控制 ----------

type platformPage struct {
	ctx      context.Context
	b        *platformBrowser
	platform string
	visible  bool
	policy   platformPolicy
	proxy    networkProxySettings

	sid       string
	mainFrame string

	mu        sync.Mutex
	topURL    string
	navigate  bool
	navSig    chan struct{}
	crashed   bool
	capture   *jsonCapture
	subs      []string
	wg        sync.WaitGroup
	navSettle time.Duration
}

func newPlatformPage(ctx context.Context, b *platformBrowser, platform string, visible bool, policy platformPolicy, proxy networkProxySettings) (*platformPage, error) {
	p := &platformPage{ctx: ctx, b: b, platform: platform, visible: visible, policy: policy, proxy: proxy, navSig: make(chan struct{}, 1)}
	r, err := b.d.call("", "Target.attachToTarget", map[string]any{"targetId": b.targetID, "flatten": true})
	if err != nil {
		return nil, fmt.Errorf("连接平台窗口失败：%w", err)
	}
	var at struct {
		SessionID string `json:"sessionId"`
	}
	_ = json.Unmarshal(r, &at)
	p.sid = at.SessionID
	b.d.setSession(p.sid, p.onEvent)
	for _, c := range []struct {
		method string
		params any
	}{
		{"Page.enable", nil},
		{"Runtime.enable", nil},
		{"Network.enable", map[string]any{"maxTotalBufferSize": jsonResponseBytesLimit, "maxResourceBufferSize": singleJSONResponseLimit}},
		{"Fetch.enable", map[string]any{"patterns": []map[string]any{{"urlPattern": "*", "requestStage": "Request"}}, "handleAuthRequests": true}},
		{"Target.setAutoAttach", map[string]any{"autoAttach": true, "waitForDebuggerOnStart": true, "flatten": true}},
	} {
		if _, err := p.call(c.method, c.params, 15*time.Second); err != nil {
			return nil, fmt.Errorf("初始化平台窗口失败（%s）：%w", c.method, err)
		}
	}
	if tree, err := p.call("Page.getFrameTree", nil, 15*time.Second); err == nil {
		var ft struct {
			FrameTree struct {
				Frame struct {
					ID string `json:"id"`
				} `json:"frame"`
			} `json:"frameTree"`
		}
		_ = json.Unmarshal(tree, &ft)
		p.mainFrame = ft.FrameTree.Frame.ID
	}
	return p, nil
}

func (p *platformPage) call(method string, params any, timeout time.Duration) (json.RawMessage, error) {
	type result struct {
		r   json.RawMessage
		err error
	}
	ch := make(chan result, 1)
	go func() {
		r, err := p.b.d.callT(p.sid, method, params, timeout)
		ch <- result{r, err}
	}()
	select {
	case r := <-ch:
		return r.r, r.err
	case <-p.ctx.Done():
		return nil, p.ctx.Err()
	case <-p.b.destroy:
		return nil, errors.New("平台窗口已关闭")
	}
}

func (p *platformPage) closed() bool { return !p.b.alive() }

func (p *platformPage) url() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.topURL
}

func (p *platformPage) onEvent(m cdpMsg) {
	switch m.Method {
	case "Page.frameNavigated":
		var fn struct {
			Frame struct {
				ParentID string `json:"parentId"`
				URL      string `json:"url"`
			} `json:"frame"`
		}
		_ = json.Unmarshal(m.Params, &fn)
		if fn.Frame.ParentID == "" && m.SessionID == p.sid {
			p.mu.Lock()
			p.topURL = fn.Frame.URL
			signal := p.navigate && fn.Frame.URL != "" && fn.Frame.URL != "about:blank"
			p.mu.Unlock()
			if signal {
				select {
				case p.navSig <- struct{}{}:
				default:
				}
			}
		}
	case "Page.navigatedWithinDocument":
		var nv struct {
			URL string `json:"url"`
		}
		_ = json.Unmarshal(m.Params, &nv)
		if m.SessionID == p.sid && nv.URL != "" {
			p.mu.Lock()
			p.topURL = nv.URL
			p.mu.Unlock()
		}
	case "Page.javascriptDialogOpening":
		var dp struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(m.Params, &dp)
		// beforeunload 必须接受；其余弹窗一律取消
		go p.b.d.call(m.SessionID, "Page.handleJavaScriptDialog", map[string]any{"accept": dp.Type == "beforeunload"})
	case "Inspector.targetCrashed":
		p.mu.Lock()
		p.crashed = true
		p.mu.Unlock()
	case "Fetch.requestPaused":
		p.wg.Add(1)
		go func(sid string) {
			defer p.wg.Done()
			p.onPaused(sid, m.Params)
		}(m.SessionID)
	case "Fetch.authRequired":
		p.wg.Add(1)
		go func(sid string) {
			defer p.wg.Done()
			p.onAuth(sid, m.Params)
		}(m.SessionID)
	case "Target.attachedToTarget":
		go p.adopt(m)
	default:
		if strings.HasPrefix(m.Method, "Network.") {
			p.onNetwork(m)
		}
	}
}

// adopt 接管页面派生的 target：弹窗直接关闭；其余（跨站子框架、worker）套上同样的拦截再放行。
func (p *platformPage) adopt(m cdpMsg) {
	var e struct {
		SessionID          string `json:"sessionId"`
		WaitingForDebugger bool   `json:"waitingForDebugger"`
		TargetInfo         struct {
			TargetID string `json:"targetId"`
			Type     string `json:"type"`
		} `json:"targetInfo"`
	}
	_ = json.Unmarshal(m.Params, &e)
	if e.TargetInfo.Type == "page" {
		_, _ = p.b.d.call("", "Target.closeTarget", map[string]any{"targetId": e.TargetInfo.TargetID})
		return
	}
	p.b.d.setSession(e.SessionID, p.onEvent)
	p.mu.Lock()
	p.subs = append(p.subs, e.SessionID)
	p.mu.Unlock()
	_, _ = p.b.d.call(e.SessionID, "Fetch.enable", map[string]any{"patterns": []map[string]any{{"urlPattern": "*", "requestStage": "Request"}}, "handleAuthRequests": true})
	_, _ = p.b.d.call(e.SessionID, "Network.enable", nil)
	_, _ = p.b.d.call(e.SessionID, "Target.setAutoAttach", map[string]any{"autoAttach": true, "waitForDebuggerOnStart": true, "flatten": true})
	if e.WaitingForDebugger {
		_, _ = p.b.d.call(e.SessionID, "Runtime.runIfWaitingForDebugger", nil)
	}
}

// electronResourceType 把 CDP 的资源类型换成 Electron webRequest 的写法，保持策略函数不变。
func electronResourceType(t string) string {
	switch t {
	case "Media":
		return "media"
	case "Image":
		return "image"
	case "Document":
		return "mainFrame"
	}
	return strings.ToLower(t)
}

func (p *platformPage) onPaused(sid string, raw json.RawMessage) {
	var e struct {
		RequestID    string `json:"requestId"`
		FrameID      string `json:"frameId"`
		ResourceType string `json:"resourceType"`
		Request      struct {
			URL string `json:"url"`
		} `json:"request"`
	}
	_ = json.Unmarshal(raw, &e)
	url := e.Request.URL
	rtype := electronResourceType(e.ResourceType)
	blocked := !p.policy.resourceURL(p.platform, url) || (p.visible && shouldBlockLoginPageRequest(p.platform, url, rtype))
	if !blocked && e.ResourceType == "Document" {
		// 主框架只能导航到官方登录流程；子框架文档在登录流程或资源白名单内即可（对应 will-navigate / will-frame-navigate）
		if e.FrameID == p.mainFrame || p.mainFrame == "" {
			blocked = !p.policy.officialFlowURL(p.platform, url)
		} else {
			blocked = !p.policy.officialFlowURL(p.platform, url) && !p.policy.resourceURL(p.platform, url)
		}
	}
	if blocked {
		_, _ = p.b.d.callT(sid, "Fetch.failRequest", map[string]any{"requestId": e.RequestID, "errorReason": "BlockedByClient"}, 10*time.Second)
		return
	}
	_, _ = p.b.d.callT(sid, "Fetch.continueRequest", map[string]any{"requestId": e.RequestID}, 10*time.Second)
}

// onAuth 只回应代理的 407 质询；站点自己的 HTTP 认证一律取消。
func (p *platformPage) onAuth(sid string, raw json.RawMessage) {
	var e struct {
		RequestID     string `json:"requestId"`
		AuthChallenge struct {
			Source string `json:"source"`
		} `json:"authChallenge"`
	}
	_ = json.Unmarshal(raw, &e)
	resp := map[string]any{"response": "CancelAuth"}
	if e.AuthChallenge.Source == "Proxy" && (p.proxy.Username != "" || p.proxy.Password != "") {
		resp = map[string]any{"response": "ProvideCredentials", "username": p.proxy.Username, "password": p.proxy.Password}
	}
	_, _ = p.b.d.callT(sid, "Fetch.continueWithAuth", map[string]any{"requestId": e.RequestID, "authChallengeResponse": resp}, 10*time.Second)
}

// ---- 导航与脚本 ----

func (p *platformPage) goto_(url string, timeout time.Duration) error {
	if p.closed() {
		return errors.New("登录窗口已关闭")
	}
	p.mu.Lock()
	p.navigate = true
	p.mu.Unlock()
	select {
	case <-p.navSig:
	default:
	}
	defer func() {
		p.mu.Lock()
		p.navigate = false
		p.mu.Unlock()
	}()
	res, err := p.call("Page.navigate", map[string]any{"url": url}, timeout)
	if err != nil {
		return fmt.Errorf("页面加载失败：%w", err)
	}
	var nv struct {
		ErrorText string `json:"errorText"`
	}
	_ = json.Unmarshal(res, &nv)
	if nv.ErrorText != "" {
		return fmt.Errorf("页面加载失败：%s", nv.ErrorText)
	}
	select {
	case <-p.navSig:
		return nil
	case <-time.After(timeout):
		return errors.New("页面导航超时")
	case <-p.ctx.Done():
		return p.ctx.Err()
	case <-p.b.destroy:
		return errors.New("Platform login window closed")
	case <-p.b.d.closed:
		return errors.New("平台浏览器已断开")
	}
}

// eval 在主框架执行表达式并按值返回；执行上下文因导航被销毁时稍等重试。
func (p *platformPage) eval(expr string) (json.RawMessage, error) {
	var lastErr error
	for attempt := 0; attempt < 6; attempt++ {
		if p.closed() {
			return nil, errors.New("登录窗口已关闭")
		}
		res, err := p.call("Runtime.evaluate", map[string]any{"expression": expr, "returnByValue": true, "awaitPromise": true}, 60*time.Second)
		if err == nil {
			var r struct {
				Result           struct{ Value json.RawMessage } `json:"result"`
				ExceptionDetails *struct {
					Text      string `json:"text"`
					Exception struct {
						Description string `json:"description"`
					} `json:"exception"`
				} `json:"exceptionDetails"`
			}
			_ = json.Unmarshal(res, &r)
			if r.ExceptionDetails != nil {
				msg := r.ExceptionDetails.Exception.Description
				if msg == "" {
					msg = r.ExceptionDetails.Text
				}
				return nil, fmt.Errorf("页面脚本异常：%s", msg)
			}
			return r.Result.Value, nil
		}
		lastErr = err
		if p.ctx.Err() != nil || !strings.Contains(strings.ToLower(err.Error()), "context") {
			break
		}
		if sleepCtx(p.ctx, 50*time.Millisecond) != nil {
			break
		}
	}
	return nil, lastErr
}

func (p *platformPage) evalBool(expr string) bool {
	v, err := p.eval(expr)
	return err == nil && string(v) == "true"
}

func (p *platformPage) evalString(expr string) (string, error) {
	v, err := p.eval(expr)
	if err != nil {
		return "", err
	}
	var s string
	if json.Unmarshal(v, &s) != nil {
		return "", errors.New("页面脚本没有返回字符串")
	}
	return s, nil
}

func (p *platformPage) content() (string, error) {
	return p.evalString(`document.documentElement.outerHTML`)
}

func (p *platformPage) scrollBy(y int) error {
	_, err := p.eval(fmt.Sprintf(`window.scrollBy({top:%d,behavior:"instant"})`, y))
	return err
}

func (p *platformPage) sleep(d time.Duration) error { return sleepCtx(p.ctx, d) }

// ---- 定位器（对应 ElectronLocator：可见性检测与点击都在页面里完成）----

type locatorQuery struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
	Exact bool   `json:"exact,omitempty"`
	Name  string `json:"name,omitempty"`
}

func locatorScript(q locatorQuery, action string) string {
	qj, _ := json.Marshal(q)
	aj, _ := json.Marshal(action)
	return `(() => {
    const query = ` + string(qj) + `;
    const normalize = (value) => String(value || "").replace(/\s+/g, " ").trim();
    const visible = (node) => {
      if (!(node instanceof HTMLElement)) return false;
      const style = window.getComputedStyle(node);
      const rect = node.getBoundingClientRect();
      return style.display !== "none" && style.visibility !== "hidden" && rect.width > 0 && rect.height > 0;
    };
    let nodes = [];
    if (query.kind === "css") {
      let selector = query.value;
      let expected = "";
      let exact = false;
      const textIs = selector.match(/:text-is\((['"])(.*?)\1\)/);
      const hasText = selector.match(/:has-text\((['"])(.*?)\1\)/);
      const match = textIs || hasText;
      if (match) {
        expected = normalize(match[2]);
        exact = Boolean(textIs);
        selector = selector.replace(match[0], "");
      }
      try { nodes = Array.from(document.querySelectorAll(selector)); } catch { nodes = []; }
      if (expected) nodes = nodes.filter((node) => exact
        ? normalize(node.textContent) === expected
        : normalize(node.textContent).includes(expected));
    } else if (query.kind === "role") {
      const selector = query.value === "button" ? "button, [role='button']" : "[role='" + query.value + "']";
      nodes = Array.from(document.querySelectorAll(selector));
      if (query.name) nodes = nodes.filter((node) => {
        const label = normalize(node.getAttribute("aria-label") || node.textContent);
        return query.exact ? label === normalize(query.name) : label.includes(normalize(query.name));
      });
    } else {
      nodes = Array.from(document.querySelectorAll("button, [role='button'], a, p, span, div"));
      nodes = nodes.filter((node) => query.exact
        ? normalize(node.textContent) === normalize(query.value)
        : normalize(node.textContent).includes(normalize(query.value)));
    }
    const node = nodes.find(visible);
    if (!node) return false;
    if (` + string(aj) + ` === "click") node.click();
    return true;
  })()`
}

func cssLoc(sel string) locatorQuery { return locatorQuery{Kind: "css", Value: sel} }
func roleLoc(role, name string, exact bool) locatorQuery {
	return locatorQuery{Kind: "role", Value: role, Name: name, Exact: exact}
}
func textLoc(text string, exact bool) locatorQuery {
	return locatorQuery{Kind: "text", Value: text, Exact: exact}
}

func (p *platformPage) visibleLoc(q locatorQuery) bool {
	return !p.closed() && p.evalBool(locatorScript(q, "visible"))
}

func (p *platformPage) waitLoc(q locatorQuery, timeout time.Duration) error {
	start := time.Now()
	for time.Since(start) < timeout {
		if p.visibleLoc(q) {
			return nil
		}
		if err := p.sleep(100 * time.Millisecond); err != nil {
			return err
		}
	}
	return errors.New("等待平台登录组件超时")
}

func (p *platformPage) clickLoc(q locatorQuery, timeout time.Duration) error {
	if err := p.waitLoc(q, timeout); err != nil {
		return err
	}
	if !p.evalBool(locatorScript(q, "click")) {
		return errors.New("平台登录入口已消失")
	}
	return nil
}

// ---- Cookie ----

type platformCookie struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Domain string `json:"domain"`
	Path   string `json:"path"`
}

func (p *platformPage) cookies(url string) ([]platformCookie, error) {
	var res json.RawMessage
	var err error
	if url != "" {
		res, err = p.call("Network.getCookies", map[string]any{"urls": []string{url}}, 15*time.Second)
	} else {
		res, err = p.call("Network.getAllCookies", nil, 15*time.Second)
	}
	if err != nil {
		return nil, err
	}
	var out struct {
		Cookies []platformCookie `json:"cookies"`
	}
	_ = json.Unmarshal(res, &out)
	return out.Cookies, nil
}

// ---- 页面自身发出的 JSON 响应（对应 startJsonCapture）----

type jsonCapture struct {
	mu       sync.Mutex
	payloads []any
	bytes    int
	pending  map[string]bool
	stopped  bool
}

func (c *jsonCapture) snapshot() []any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]any(nil), c.payloads...)
}

func (p *platformPage) startJSONCapture() *jsonCapture {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.capture == nil {
		p.capture = &jsonCapture{pending: map[string]bool{}}
	}
	return p.capture
}

func (p *platformPage) stopJSONCapture() {
	p.mu.Lock()
	c := p.capture
	p.capture = nil
	p.mu.Unlock()
	if c != nil {
		c.mu.Lock()
		c.stopped = true
		c.mu.Unlock()
	}
}

func (p *platformPage) onNetwork(m cdpMsg) {
	p.mu.Lock()
	c := p.capture
	p.mu.Unlock()
	if c == nil {
		return
	}
	var e struct {
		RequestID string `json:"requestId"`
		Type      string `json:"type"`
		Response  struct {
			URL      string `json:"url"`
			MimeType string `json:"mimeType"`
		} `json:"response"`
	}
	_ = json.Unmarshal(m.Params, &e)
	switch m.Method {
	case "Network.responseReceived":
		mime := strings.ToLower(e.Response.MimeType)
		if e.Response.URL != "" && p.policy.allowedPlatformURL(p.platform, e.Response.URL) &&
			(strings.Contains(mime, "json") || strings.Contains(mime, "javascript") || e.Type == "XHR" || e.Type == "Fetch") {
			c.mu.Lock()
			c.pending[e.RequestID] = true
			c.mu.Unlock()
		}
	case "Network.loadingFailed":
		c.mu.Lock()
		delete(c.pending, e.RequestID)
		c.mu.Unlock()
	case "Network.loadingFinished":
		c.mu.Lock()
		ok := c.pending[e.RequestID] && !c.stopped && len(c.payloads) < jsonResponseLimit
		delete(c.pending, e.RequestID)
		c.mu.Unlock()
		if !ok {
			return
		}
		sid := m.SessionID
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			res, err := p.b.d.callT(sid, "Network.getResponseBody", map[string]any{"requestId": e.RequestID}, 15*time.Second)
			if err != nil {
				return
			}
			var body struct {
				Body          string `json:"body"`
				Base64Encoded bool   `json:"base64Encoded"`
			}
			_ = json.Unmarshal(res, &body)
			raw := []byte(body.Body)
			if body.Base64Encoded {
				if raw, err = base64.StdEncoding.DecodeString(body.Body); err != nil {
					return
				}
			}
			v, err := jsParse(string(raw))
			if err != nil { // 非 JSON 的脚本响应不进入采集结果
				return
			}
			c.mu.Lock()
			defer c.mu.Unlock()
			if c.stopped || len(c.payloads) >= jsonResponseLimit || len(raw) > singleJSONResponseLimit || c.bytes+len(raw) > jsonResponseBytesLimit {
				return
			}
			c.payloads = append(c.payloads, v)
			c.bytes += len(raw)
		}()
	}
}

// close 收尾：停止采集、等拦截协程结束（最多 5 秒）。
func (p *platformPage) close() {
	p.stopJSONCapture()
	done := make(chan struct{})
	go func() { p.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
	p.b.d.setSession(p.sid, nil)
	p.mu.Lock()
	subs := p.subs
	p.mu.Unlock()
	for _, s := range subs {
		p.b.d.setSession(s, nil)
	}
}
