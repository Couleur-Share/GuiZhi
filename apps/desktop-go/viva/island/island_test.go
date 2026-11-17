package island

import (
	"os"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
)

func serveNothing(string) (string, []byte, bool) { return "", nil, false }

// 固化的安全与性能默认值：任何一项被改掉都应当让测试变红，而不是悄悄放宽。
func TestWebViewConfigDefaults(t *testing.T) {
	wc, err := webViewConfig(Config{Serve: serveNothing})
	if err != nil {
		t.Fatal(err)
	}
	if wc.URL != "gzisland://doc/index.html" {
		t.Errorf("URL = %q", wc.URL)
	}
	if wc.Scheme == nil || wc.Scheme.Name != DefaultScheme {
		t.Errorf("scheme = %+v", wc.Scheme)
	}
	if wc.Proxy != app.WebProxyDirect {
		t.Errorf("W1 要求岛使用 Proxy 非空的独立环境，实际 %q", wc.Proxy)
	}
	if !wc.Ephemeral {
		t.Error("岛必须是 Ephemeral")
	}
	if !wc.BridgeMainFrameOnly {
		t.Error("岛必须只在主框架注入桥")
	}
	if wc.NewWindow != nil {
		t.Error("NewWindow 必须为空（即全部拒绝）")
	}
	if wc.Download != nil {
		t.Error("Download 必须为空（即全部拒绝）")
	}
	if wc.Navigate == nil {
		t.Fatal("Navigate 必须设置，否则 Viva 放行所有导航")
	}
}

func TestWebViewConfigRequiresServe(t *testing.T) {
	if _, err := webViewConfig(Config{}); err == nil {
		t.Fatal("没有 Serve 应当报错")
	}
}

func TestWebViewConfigCustomSchemeAndEntry(t *testing.T) {
	wc, err := webViewConfig(Config{Scheme: "gzwiki", Entry: "graph.html", Serve: serveNothing})
	if err != nil {
		t.Fatal(err)
	}
	if wc.URL != "gzwiki://doc/graph.html" || wc.Scheme.Name != "gzwiki" {
		t.Errorf("URL=%q scheme=%q", wc.URL, wc.Scheme.Name)
	}
	if !wc.Navigate(app.WebNavigation{URL: "gzwiki://doc/graph.html", MainFrame: true}) {
		t.Error("自有 scheme 的主框架导航应当放行")
	}
	if wc.Navigate(app.WebNavigation{URL: "gzisland://doc/index.html", MainFrame: true}) {
		t.Error("别的 scheme（即使是默认名）不应放行")
	}
}

func TestNavigateReportsDenied(t *testing.T) {
	var gotURL string
	var gotMain bool
	wc, _ := webViewConfig(Config{Serve: serveNothing, OnDenied: func(u string, main bool) { gotURL, gotMain = u, main }})
	if wc.Navigate(app.WebNavigation{URL: "https://example.com/", MainFrame: true}) {
		t.Fatal("外部地址必须拒绝")
	}
	if gotURL != "https://example.com/" || !gotMain {
		t.Errorf("OnDenied 没有收到被拒的地址：%q main=%v", gotURL, gotMain)
	}
	gotURL = ""
	if !wc.Navigate(app.WebNavigation{URL: "about:srcdoc", MainFrame: false}) {
		t.Fatal("内层文档 about:srcdoc 应当放行")
	}
	if gotURL != "" {
		t.Error("放行的导航不应触发 OnDenied")
	}
}

// 没有 WebView2 的环境里 Open 必然失败；这里验证失败路径也把 W1 的环境变量还原了，
// 并且不会把参数留给后面的页面。
func TestOpenRestoresBrowserArgsOnFailure(t *testing.T) {
	t.Setenv(browserArgsVar, "--existing-flag")
	if _, err := Open(&app.WebHost{}, Config{Serve: serveNothing}); err == nil {
		t.Fatal("没有绑定窗口的 WebHost 应当打开失败")
	}
	if got := os.Getenv(browserArgsVar); got != "--existing-flag" {
		t.Errorf("环境变量没有还原：%q", got)
	}

	os.Unsetenv(browserArgsVar)
	if _, err := Open(&app.WebHost{}, Config{Serve: serveNothing}); err == nil {
		t.Fatal("没有绑定窗口的 WebHost 应当打开失败")
	}
	if v, ok := os.LookupEnv(browserArgsVar); ok {
		t.Errorf("原本没设的变量被留下了：%q", v)
	}
}

func serveOne(path string) (string, []byte, bool) {
	if path == "a.html" {
		return "text/html; charset=utf-8", []byte("<p>x</p>"), true
	}
	return "", nil, false
}

// 每个响应（含 404）都带加固头；内容与 MIME 原样，状态码不被改写。
func TestSchemeResponsesCarryHardenedHeaders(t *testing.T) {
	wc, err := webViewConfig(Config{Serve: serveOne})
	if err != nil {
		t.Fatal(err)
	}
	if wc.Scheme.Respond == nil || wc.Scheme.Serve == nil {
		t.Fatal("Respond 必须设置（带头的路径），Serve 保留给不支持 Respond 的平台")
	}
	want := map[string]string{
		"Content-Security-Policy": DefaultCSP,
		"Permissions-Policy":      DefaultPermissionsPolicy,
		"Referrer-Policy":         "no-referrer",
		"X-Content-Type-Options":  "nosniff",
	}
	ok200, found := wc.Scheme.Respond(app.WebRequest{Path: "a.html"})
	if !found || ok200.Status != 0 || ok200.MIME != "text/html; charset=utf-8" || string(ok200.Data) != "<p>x</p>" {
		t.Fatalf("200 响应被改写：%+v found=%v", ok200, found)
	}
	missing, found := wc.Scheme.Respond(app.WebRequest{Path: "nope.html"})
	if found || missing.Status != 404 {
		t.Fatalf("404 响应：%+v found=%v", missing, found)
	}
	for name, value := range want {
		if ok200.Headers[name] != value {
			t.Errorf("200 响应的 %s = %q，期望 %q", name, ok200.Headers[name], value)
		}
		if missing.Headers[name] != value {
			t.Errorf("404 响应的 %s = %q，期望 %q", name, missing.Headers[name], value)
		}
	}
}

// 响应头不共享：改掉上一次返回的 map，不能影响下一次。
func TestResponseHeadersAreNotShared(t *testing.T) {
	wc, _ := webViewConfig(Config{Serve: serveOne})
	first, _ := wc.Scheme.Respond(app.WebRequest{Path: "a.html"})
	first.Headers["Content-Security-Policy"] = "tampered"
	delete(first.Headers, "Permissions-Policy")
	second, _ := wc.Scheme.Respond(app.WebRequest{Path: "a.html"})
	if second.Headers["Content-Security-Policy"] != DefaultCSP || second.Headers["Permissions-Policy"] == "" {
		t.Fatalf("上一次响应的头被后一次继承了：%+v", second.Headers)
	}
}

// Config.CSP 覆盖默认策略（探针页面是内联脚本，需要 script-src 'unsafe-inline'）；其余头不受影响。
func TestConfigCSPOverridesDefault(t *testing.T) {
	const custom = "default-src 'none'; script-src 'unsafe-inline'"
	wc, _ := webViewConfig(Config{Serve: serveOne, CSP: custom})
	got, _ := wc.Scheme.Respond(app.WebRequest{Path: "a.html"})
	if got.Headers["Content-Security-Policy"] != custom {
		t.Errorf("CSP = %q", got.Headers["Content-Security-Policy"])
	}
	if got.Headers["Permissions-Policy"] != DefaultPermissionsPolicy {
		t.Error("覆盖 CSP 不应改动 Permissions-Policy")
	}
}

func cspDirectives(policy string) map[string][]string {
	out := map[string][]string{}
	for _, part := range strings.Split(policy, ";") {
		fields := strings.Fields(part)
		if len(fields) > 0 {
			out[fields[0]] = fields[1:]
		}
	}
	return out
}

// 默认策略必须是关死的：脚本只许自有 scheme 的文件，出站网络与框架关闭，任何指令里都不能有通配或网络来源。
// 放宽任何一项都应当让这个测试变红，而不是悄悄通过。
func TestDefaultCSPIsClosed(t *testing.T) {
	d := cspDirectives(DefaultCSP)
	exact := map[string]string{
		"default-src": "'none'", "script-src": "'self'", "connect-src": "'self'", "frame-src": "about:",
		"object-src": "'none'", "base-uri": "'none'", "form-action": "'none'",
	}
	for name, source := range exact {
		if got := strings.Join(d[name], " "); got != source {
			t.Errorf("%s = %q，期望 %q", name, got, source)
		}
	}
	allowed := map[string]bool{"'none'": true, "'self'": true, "'unsafe-inline'": true, "data:": true, "blob:": true, "about:": true}
	for name, sources := range d {
		for _, source := range sources {
			if !allowed[source] {
				t.Errorf("%s 里出现了不在白名单里的来源 %q", name, source)
			}
			if source == "'unsafe-inline'" && name != "style-src" {
				t.Errorf("只有 style-src 允许 'unsafe-inline'，%s 也有", name)
			}
		}
	}
	if len(d["style-src"]) == 0 {
		t.Error("缺 style-src")
	}
}

// 会弹权限提示或接触硬件的特性必须关；复制按钮与媒体播放要用的特性不能被关掉。
func TestPermissionsPolicyClosesPromptingFeatures(t *testing.T) {
	for _, feature := range []string{"camera", "microphone", "geolocation", "clipboard-read", "usb", "serial", "hid", "midi", "payment", "display-capture", "bluetooth"} {
		if !strings.Contains(DefaultPermissionsPolicy, feature+"=()") {
			t.Errorf("Permissions-Policy 没有关掉 %s", feature)
		}
	}
	for _, feature := range []string{"clipboard-write", "fullscreen", "autoplay"} {
		if strings.Contains(DefaultPermissionsPolicy, feature) {
			t.Errorf("%s 不应被关闭（阅读视图的复制按钮与媒体播放要用）", feature)
		}
	}
}

func TestComponentUpdateArgIsTheOneW1Verified(t *testing.T) {
	// W1 的范围验证（计划 §10.9）针对的是这个参数；换成别的参数要重新验证范围，不能顺手改。
	if !strings.EqualFold(componentUpdateOff, "--disable-component-update") {
		t.Errorf("W1 参数被改动：%q", componentUpdateOff)
	}
}
