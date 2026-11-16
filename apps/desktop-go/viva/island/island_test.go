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

func TestComponentUpdateArgIsTheOneW1Verified(t *testing.T) {
	// W1 的范围验证（计划 §10.9）针对的是这个参数；换成别的参数要重新验证范围，不能顺手改。
	if !strings.EqualFold(componentUpdateOff, "--disable-component-update") {
		t.Errorf("W1 参数被改动：%q", componentUpdateOff)
	}
}
