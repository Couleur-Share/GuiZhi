package main

// 登录态平台采集服务：对应 platform-capture/browser-capture.ts 的 BrowserCaptureService。
// 差异（登记于 MYGO-MIGRATION.md）：浏览器由 Electron BrowserWindow 换成 WebView2 子进程 + CDP；
// 登录态状态文件改名为 session-status-webview2.json（两种壳的会话档案不通用，切换后需重新登录一次）；
// 平台发现（discover / search）已移除。

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	platformLoginTimeout      = 5 * time.Minute
	loginPanelAutoOpenTimeout = 10 * time.Second
	loginPanelClickTimeout    = 3 * time.Second
	loginPanelConfirmTimeout  = 5 * time.Second
	loginPanelPollInterval    = 250 * time.Millisecond
	douyinFastLoginReadyWait  = 8 * time.Second
	douyinFastLoginURL        = "https://creator.douyin.com/creator-micro/interactive/comment"
	platformNavigationTimeout = 45 * time.Second
	platformSessionStatusVer  = 1
	platformSessionStatusFile = "session-status-webview2.json"
	platformWindowEnv         = "GUIZHI_PLATFORM_WINDOW"
	platformBrowserDirPrefix  = "webview2-"
)

// platformCaptureError 对应 PlatformCaptureError：错误文本带 [code] 前缀，导入任务据此判定登录失效等状态。
type platformCaptureError struct{ Code, Message string }

func (e *platformCaptureError) Error() string { return "[" + e.Code + "] " + e.Message }

func pcErr(code, msg string) error { return &platformCaptureError{Code: code, Message: msg} }

func isPlatformErr(err error, code string) bool {
	var pe *platformCaptureError
	return errors.As(err, &pe) && pe.Code == code
}

type platformOp struct {
	cancel   context.CancelFunc
	kind     string
	platform string
}

type platformSessions struct {
	l       *Library
	baseDir string
	policy  platformPolicy
	exe     string // 测试里指向测试二进制以外的可执行文件时使用；空取当前 exe
	window  string // 非登录操作与登录的窗口模式覆盖；空按默认（登录可见、其余屏幕外）

	detailPollCount int                         // 抖音详情轮询次数（0=40，测试缩短）
	nodeseekTopic   func(string) (string, bool) // 测试替换帖子地址判定（模拟站点）
	loginTimeout    time.Duration
	loginPoll       time.Duration

	sem chan struct{}

	mu             sync.Mutex
	active         *platformOp
	nodeseekCancel context.CancelFunc
	version        string
	stateMu        sync.Mutex
}

func newPlatformSessions(l *Library, userDataDir string) *platformSessions {
	return &platformSessions{
		l: l, baseDir: filepath.Join(userDataDir, "browser-capture"), policy: defaultPlatformPolicy(),
		loginTimeout: platformLoginTimeout, loginPoll: time.Second, sem: make(chan struct{}, 1),
	}
}

func (s *platformSessions) profileDir(platform string) string {
	return filepath.Join(s.baseDir, platformBrowserDirPrefix+platform)
}

// ---- 登录态标记 ----

func (s *platformSessions) readState() map[string]bool {
	out := map[string]bool{}
	raw, err := os.ReadFile(filepath.Join(s.baseDir, platformSessionStatusFile))
	if err != nil {
		return out
	}
	var st map[string]any
	if json.Unmarshal(raw, &st) != nil {
		return out
	}
	if v, _ := st["version"].(float64); int(v) != platformSessionStatusVer {
		return out
	}
	for _, p := range platformIDs {
		if b, _ := st[p].(bool); b {
			out[p] = true
		}
	}
	return out
}

func (s *platformSessions) writeLoginState(platform string, loggedIn bool) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	state := s.readState()
	state[platform] = loggedIn
	obj := newObj()
	obj.set("version", float64(platformSessionStatusVer))
	for _, p := range platformIDs {
		obj.set(p, state[p])
	}
	if err := os.MkdirAll(s.baseDir, 0o700); err != nil {
		logAppErrorf("platform-capture", "保存平台登录标记", "%v", err)
		return
	}
	raw, _ := jsStringify(obj)
	if err := os.WriteFile(filepath.Join(s.baseDir, platformSessionStatusFile), []byte(raw), 0o600); err != nil {
		logAppErrorf("platform-capture", "保存平台登录标记", "%v", err)
	}
}

func (s *platformSessions) requireKnownLogin(platform string) error {
	if !s.readState()[platform] {
		return pcErr("login_required", "请先在设置或平台发现中登录账号")
	}
	return nil
}

func (s *platformSessions) statuses() []any {
	state := s.readState()
	s.mu.Lock()
	active, version := s.active, s.version
	s.mu.Unlock()
	out := make([]any, 0, len(platformIDs))
	for _, p := range platformIDs {
		o := newObj()
		o.set("platform", p)
		o.set("browser", "embedded")
		if version != "" {
			o.set("browserVersion", version)
		}
		o.set("available", true)
		o.set("loggedIn", state[p])
		o.set("busy", active != nil && active.platform == p)
		out = append(out, o)
	}
	return out
}

func (s *platformSessions) status(platform string) any {
	for _, v := range s.statuses() {
		if jsGet(v, "platform") == platform {
			return v
		}
	}
	return nil
}

// ---- 串行执行 ----

func (s *platformSessions) cancel(kind string) bool {
	s.mu.Lock()
	op := s.active
	s.mu.Unlock()
	if op == nil || (kind != "" && op.kind != kind) {
		return false
	}
	op.cancel()
	return true
}

func (s *platformSessions) cancelForPlatform(kind, platform string) bool {
	s.mu.Lock()
	nc := s.nodeseekCancel
	op := s.active
	s.mu.Unlock()
	if kind == "login" && platform == "nodeseek" && nc != nil {
		nc()
		return true
	}
	if op == nil || op.platform != platform {
		return false
	}
	return s.cancel(kind)
}

type platformRun struct {
	visible bool
	title   string
	wipe    bool // 启动前清空该平台会话（强制重新登录）
}

func (s *platformSessions) windowMode(visible bool) string {
	if s.window != "" {
		return s.window
	}
	if v := os.Getenv(platformWindowEnv); v == "offscreen" {
		return "offscreen"
	}
	if visible {
		return "visible"
	}
	return "offscreen"
}

func (s *platformSessions) acquire(ctx context.Context) error {
	select {
	case s.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return pcErr("canceled", "操作已取消")
	}
}

func (s *platformSessions) runSerialized(parent context.Context, platform, kind string, run platformRun, fn func(ctx context.Context, p *platformPage) error) error {
	if err := s.acquire(parent); err != nil {
		return err
	}
	defer func() { <-s.sem }()
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	op := &platformOp{cancel: cancel, kind: kind, platform: platform}
	s.mu.Lock()
	s.active = op
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.active == op {
			s.active = nil
		}
		s.mu.Unlock()
	}()
	if run.wipe {
		if err := s.removeProfile(platform); err != nil {
			return err
		}
	}
	proxy := activeNetProxy.Settings()
	b, err := startPlatformBrowser(ctx, platformBrowserOptions{
		Exe: s.exe, UserData: s.profileDir(platform), Window: s.windowMode(run.visible), Title: run.title, ProxyArgs: platformProxyArgs(proxy),
	})
	if err != nil {
		if ctx.Err() != nil {
			return pcErr("canceled", "操作已取消")
		}
		return pcErr("browser_unavailable", "平台浏览器启动失败："+err.Error())
	}
	s.mu.Lock()
	if b.version != "" {
		s.version = b.version
	}
	s.mu.Unlock()
	page, err := newPlatformPage(ctx, b, platform, run.visible, s.policy, proxy)
	if err != nil {
		b.kill()
		if ctx.Err() != nil {
			return pcErr("canceled", "操作已取消")
		}
		return pcErr("browser_unavailable", err.Error())
	}
	ferr := fn(ctx, page)
	page.close()
	if ctx.Err() != nil || !b.alive() {
		b.kill()
	} else {
		b.stop()
	}
	if ferr != nil && ctx.Err() != nil && !isPlatformErr(ferr, "canceled") {
		if kind == "login" {
			return pcErr("canceled", "已取消登录")
		}
		return pcErr("canceled", "操作已取消")
	}
	return ferr
}

// removeProfile 清空某平台的会话目录；浏览器必须已退出（调用方持有串行锁）。
func (s *platformSessions) removeProfile(platform string) error {
	dir := filepath.Clean(s.profileDir(platform))
	if filepath.Dir(dir) != filepath.Clean(s.baseDir) || !strings.HasPrefix(filepath.Base(dir), platformBrowserDirPrefix) {
		return errors.New("拒绝清理非归知内置平台会话目录")
	}
	if err := removeAllRetry(dir, 30, 250*time.Millisecond); err != nil {
		return err
	}
	return nil
}

func (s *platformSessions) logout(ctx context.Context, platform string) error {
	if err := s.acquire(ctx); err != nil {
		return err
	}
	defer func() { <-s.sem }()
	if err := s.removeProfile(platform); err != nil {
		return err
	}
	s.writeLoginState(platform, false)
	return nil
}

func (s *platformSessions) clearAll(ctx context.Context) error {
	s.cancel("")
	if err := s.acquire(ctx); err != nil {
		return err
	}
	defer func() { <-s.sem }()
	resolved := filepath.Clean(s.baseDir)
	if filepath.Base(resolved) != "browser-capture" {
		return errors.New("拒绝清理非归知内置平台会话目录")
	}
	return removeAllRetry(resolved, 30, 250*time.Millisecond)
}

func (s *platformSessions) close() {
	s.cancel("")
	// 等进行中的操作退出（其浏览器会随取消被回收）
	select {
	case s.sem <- struct{}{}:
		<-s.sem
	case <-time.After(15 * time.Second):
	}
}

// ---- 登录态判定 ----

func (s *platformSessions) hasLoginCookies(p *platformPage, platform string) bool {
	checkURL := s.policy.loginURL(platform)
	if platform == "linuxdo" {
		checkURL = "https://linux.do/"
	}
	cookies, err := p.cookies(checkURL)
	if err != nil {
		return false
	}
	if len(cookies) == 0 {
		if cookies, err = p.cookies(""); err != nil {
			return false
		}
	}
	for _, c := range cookies {
		for _, n := range platformLoginCookieNames[platform] {
			if c.Name == n && c.Value != "" {
				return true
			}
		}
	}
	return false
}

func (s *platformSessions) loginCookieSnapshot(p *platformPage, platform string) map[string]string {
	snap := map[string]string{}
	cookies, _ := p.cookies(s.policy.loginURL(platform))
	for _, c := range cookies {
		for _, n := range platformLoginCookieNames[platform] {
			if c.Name == n && c.Value != "" {
				snap[c.Name] = c.Value
			}
		}
	}
	return snap
}

// didLoginCookieSnapshotChange 对应同名函数：任一登录 Cookie 出现了新的非空值。
func didLoginCookieSnapshotChange(before, after map[string]string) bool {
	for name, v := range after {
		if v != "" && before[name] != v {
			return true
		}
	}
	return false
}

var xhsSelfProfile = cssLoc(`a[href*="/user/profile/"] span:text-is("我")`)

func (s *platformSessions) hasLoginState(p *platformPage, platform string, xhsBaseline map[string]string) bool {
	switch platform {
	case "linuxdo":
		return s.hasLoginCookies(p, platform) || p.evalBool(`Boolean(document.querySelector("#current-user, .current-user"))`)
	case "douyin":
		return s.hasLoginCookies(p, platform) || p.evalBool(`window.localStorage.getItem("HasUserLogin") === "1"`)
	}
	// 任意作者主页链接在未登录信息流里也大量存在，只认侧栏中指向本人主页且文本为“我”的账号入口
	if p.visibleLoc(xhsSelfProfile) {
		return true
	}
	if xhsBaseline != nil {
		return didLoginCookieSnapshotChange(xhsBaseline, s.loginCookieSnapshot(p, platform))
	}
	return false
}

func (s *platformSessions) hasExistingLoginState(p *platformPage, platform string) bool {
	if platform == "douyin" || platform == "linuxdo" {
		return s.hasLoginState(p, platform, nil)
	}
	return p.waitLoc(xhsSelfProfile, 2500*time.Millisecond) == nil
}

func (s *platformSessions) xhsGuestCookieBaseline(p *platformPage) map[string]string {
	// 等二维码或手机号表单就绪后再取基线，避免把页面初始化时写入的访客 web_session 误认为扫码带来的变化
	_ = p.waitLoc(cssLoc("img.qrcode-img, div.login-container input[placeholder*='手机号'], div.login-container input[placeholder*='验证码']"), 10*time.Second)
	return s.loginCookieSnapshot(p, "xiaohongshu")
}

func (s *platformSessions) openLoginPanel(p *platformPage, platform string) {
	var panel locatorQuery
	var candidates []locatorQuery
	if platform == "douyin" {
		panel = cssLoc("#login-panel-new, #login-panel, [class*='login-panel']")
		candidates = []locatorQuery{cssLoc(`p:text-is("登录")`), roleLoc("button", "登录", true), cssLoc(`[role="button"]:has-text("登录")`), textLoc("登录", true)}
	} else {
		panel = cssLoc("div.login-container, [class*='login-container'], [class*='login-modal']")
		candidates = []locatorQuery{roleLoc("button", "登录", true), textLoc("登录", true)}
	}
	// 同时观察面板和首选登录入口：入口一旦挂载就立即点击，不再固定等满 10 秒
	preferred := candidates[0]
	start := time.Now()
	for time.Since(start) < loginPanelAutoOpenTimeout {
		if p.visibleLoc(panel) {
			return
		}
		if p.visibleLoc(preferred) {
			if p.clickLoc(preferred, loginPanelClickTimeout) == nil && p.waitLoc(panel, loginPanelConfirmTimeout) == nil {
				return
			}
			break
		}
		if p.sleep(loginPanelPollInterval) != nil {
			return
		}
	}
	for _, c := range candidates {
		if p.waitLoc(c, loginPanelClickTimeout) == nil && p.clickLoc(c, loginPanelClickTimeout) == nil && p.waitLoc(panel, loginPanelConfirmTimeout) == nil {
			return
		}
	}
	// 保持窗口开启，让用户仍可在官方页面手动操作；登录状态轮询继续运行
}

func (s *platformSessions) hasReadyFastLoginSurface(p *platformPage, platform string) bool {
	if platform != "douyin" {
		return false
	}
	return p.waitLoc(textLoc("扫码登录", true), douyinFastLoginReadyWait) == nil
}

// ---- 登录 ----

func platformLoginTitle(platform string) string {
	switch platform {
	case "xiaohongshu":
		return "小红书官方登录"
	case "douyin":
		return "抖音官方登录"
	case "nodeseek":
		return "NodeSeek 验证 · 完成后自动采集"
	}
	return "LINUX DO 验证"
}

func (s *platformSessions) login(ctx context.Context, platform string, forceRelogin bool, verificationURL string) (any, error) {
	if platform == "nodeseek" {
		s.mu.Lock()
		if s.nodeseekCancel != nil {
			s.mu.Unlock()
			return nil, pcErr("verification_required", "NodeSeek 已有验证进行中，请先完成或取消")
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		s.nodeseekCancel = cancel
		s.mu.Unlock()
		defer func() {
			cancel()
			s.mu.Lock()
			s.nodeseekCancel = nil
			s.mu.Unlock()
		}()
	}
	err := s.runSerialized(ctx, platform, "login", platformRun{visible: true, title: platformLoginTitle(platform), wipe: forceRelogin}, func(ctx context.Context, p *platformPage) error {
		if platform == "nodeseek" {
			target, err := nodeseekVerificationURL(verificationURL)
			if err != nil {
				return err
			}
			if _, err := s.readNodeseekPage(ctx, p, target, true); err != nil {
				return err
			}
			s.writeLoginState(platform, true)
			return nil
		}
		if forceRelogin {
			s.writeLoginState(platform, false)
		}
		entry := s.policy.loginURL(platform)
		if platform == "douyin" {
			entry = douyinFastLoginURL
		}
		if err := p.goto_(entry, platformNavigationTimeout); err != nil {
			return err
		}
		if !forceRelogin && s.hasExistingLoginState(p, platform) {
			s.writeLoginState(platform, true)
			return nil
		}
		s.showLoginLoadingIndicator(p, platform)
		if !s.hasReadyFastLoginSurface(p, platform) {
			if platform == "douyin" {
				// 仅抖音存在创作者中心快速入口；入口改版或暂不可用时才回退消费端首页
				if err := p.goto_(s.policy.loginURL(platform), platformNavigationTimeout); err != nil {
					return err
				}
				s.showLoginLoadingIndicator(p, platform)
			}
			if platform != "linuxdo" {
				s.openLoginPanel(p, platform)
			}
		}
		var baseline map[string]string
		if platform == "xiaohongshu" {
			baseline = s.xhsGuestCookieBaseline(p)
		}
		start := time.Now()
		for time.Since(start) < s.loginTimeout {
			if ctx.Err() != nil {
				return pcErr("canceled", "已取消登录")
			}
			if s.hasLoginState(p, platform, baseline) {
				s.writeLoginState(platform, true)
				return nil
			}
			if p.closed() {
				return pcErr("browser_closed", "平台登录窗口已关闭")
			}
			if err := p.sleep(s.loginPoll); err != nil {
				return pcErr("canceled", "已取消登录")
			}
		}
		return pcErr("login_timeout", "登录等待已超过 5 分钟")
	})
	if err != nil {
		return nil, err
	}
	return s.status(platform), nil
}

// ---- 采集 ----

type capturedPage struct {
	HTML         string
	FinalURL     string
	JSONPayloads []any
}

var reLoginPath = regexp.MustCompile(`(?i)/(?:login|passport)(?:/|$)`)

func isLoginPage(raw string) bool {
	u, err := parseWhatwgURL(raw)
	return err == nil && reLoginPath.MatchString(u.Pathname())
}

var (
	reLoginMessage = regexp.MustCompile(`(?i)login[_ -]?required|not[_ -]?login|未登录|登录(?:已)?失效|请.*登录`)
	reLoginCodeMsg = regexp.MustCompile(`(?i)login|登录`)
)

// indicatesLoginRequired 对应同名函数：页面自己的接口返回“未登录”。
func indicatesLoginRequired(v any, depth int) bool {
	if depth > 8 || v == nil || v == jsNull || v == jsUndefined {
		return false
	}
	if arr, ok := v.(jsArr); ok {
		for _, e := range arr {
			if indicatesLoginRequired(e, depth+1) {
				return true
			}
		}
		return false
	}
	o, ok := v.(*jsObj)
	if !ok {
		return false
	}
	pick := func(keys ...string) any {
		for _, k := range keys {
			if x := o.get(k); !isNullish(x) {
				return x
			}
		}
		return nil
	}
	message := ""
	if m := pick("message", "msg", "status_msg"); m != nil {
		message = cleanPlatformText(m, 300)
	}
	code := ""
	if c := pick("code", "status_code", "error_code"); c != nil {
		code = jsToString(c)
	}
	if reLoginMessage.MatchString(message) {
		return true
	}
	if (code == "10001" || code == "10002" || code == "-100" || code == "-101") && reLoginCodeMsg.MatchString(message) {
		return true
	}
	for _, k := range o.keys() {
		if indicatesLoginRequired(o.get(k), depth+1) {
			return true
		}
	}
	return false
}

func (s *platformSessions) requireLogin(p *platformPage, platform string) error {
	if !s.hasLoginCookies(p, platform) {
		s.writeLoginState(platform, false)
		return pcErr("login_required", "平台登录状态已失效，请重新登录")
	}
	return nil
}

func (s *platformSessions) assertFinalURL(platform, url string) error {
	if !s.policy.officialFlowURL(platform, url) {
		return pcErr("platform_changed", "平台跳转到了非预期地址")
	}
	if !s.policy.allowedPlatformURL(platform, url) {
		s.writeLoginState(platform, false)
		return pcErr("login_required", "平台要求重新验证登录状态")
	}
	return nil
}

func (s *platformSessions) loginExpired(platform string) error {
	s.writeLoginState(platform, false)
	return pcErr("login_required", "平台登录状态已失效，请重新登录")
}

// capturePage 对应 capturePage：用已登录会话打开页面，返回 HTML 与页面自己发出的 JSON 响应。
func (s *platformSessions) capturePage(ctx context.Context, platform, url string) (*capturedPage, error) {
	if !s.policy.allowedPlatformURL(platform, url) {
		return nil, errors.New("不支持的平台地址")
	}
	if err := s.requireKnownLogin(platform); err != nil {
		return nil, err
	}
	var out *capturedPage
	err := s.runSerialized(ctx, platform, "capture", platformRun{}, func(ctx context.Context, p *platformPage) error {
		if err := s.requireLogin(p, platform); err != nil {
			return err
		}
		cap := p.startJSONCapture()
		if err := p.goto_(url, platformNavigationTimeout); err != nil {
			return err
		}
		if err := s.assertFinalURL(platform, p.url()); err != nil {
			return err
		}
		if err := p.sleep(1500 * time.Millisecond); err != nil {
			return pcErr("canceled", "操作已取消")
		}
		payloads := cap.snapshot()
		if isLoginPage(p.url()) || anyLoginRequired(payloads) {
			return s.loginExpired(platform)
		}
		html, err := p.content()
		if err != nil {
			return err
		}
		out = &capturedPage{HTML: html, FinalURL: p.url(), JSONPayloads: payloads}
		return nil
	})
	return out, err
}

func anyLoginRequired(payloads []any) bool {
	for _, v := range payloads {
		if indicatesLoginRequired(v, 0) {
			return true
		}
	}
	return false
}

// fetchJSONViaSession 对应 fetchJsonViaSession：携带平台会话 Cookie 读取 JSON 接口（LINUX DO 等需过 Cloudflare 的 Discourse）。
// Electron 版用 session.fetch；平台浏览器没有等价 API，改为在隐藏窗口里直接打开该 JSON 地址并读取文档文本。
func (s *platformSessions) fetchJSONViaSession(ctx context.Context, platform, url string) (any, error) {
	if !s.policy.allowedPlatformURL(platform, url) {
		return nil, errors.New("不支持的平台地址")
	}
	var out any
	err := s.runSerialized(ctx, platform, "capture", platformRun{}, func(ctx context.Context, p *platformPage) error {
		if err := p.goto_(url, platformNavigationTimeout); err != nil {
			return err
		}
		meta, err := p.evalString(`JSON.stringify({type:document.contentType||"",status:(performance.getEntriesByType("navigation")[0]||{}).responseStatus||0})`)
		if err != nil {
			return err
		}
		var m struct {
			Type   string `json:"type"`
			Status int    `json:"status"`
		}
		_ = json.Unmarshal([]byte(meta), &m)
		if m.Status >= 400 {
			return errors.New("HTTP " + jsToString(float64(m.Status)))
		}
		lt := strings.ToLower(m.Type)
		if lt != "" && !strings.Contains(lt, "json") && !strings.Contains(lt, "text/plain") {
			return errors.New("接口未返回 JSON: " + strings.SplitN(m.Type, ";", 2)[0])
		}
		text, err := p.evalString(`document.body ? document.body.innerText : ""`)
		if err != nil {
			return err
		}
		v, err := jsParse(text)
		if err != nil {
			return errors.New("接口响应不是合法的 JSON")
		}
		out = v
		return nil
	})
	return out, err
}

func cleanPlatformText(v any, max int) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	s = reCtrlChars.ReplaceAllString(s, "")
	return utf16Slice(jsTrim(s), max)
}

var reCtrlChars = regexp.MustCompile("[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]")
