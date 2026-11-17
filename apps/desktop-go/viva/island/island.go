// Package island 是「岛」的 W1 封装：Viva 窗口里托管的一个 WebView2 页面。
//
// 原生界面空闲时不存在任何 WebView；只有阅读视图、Wiki 图谱、mermaid、媒体播放这类必须由浏览器引擎
// 渲染的内容才按需开一个岛，用完立即销毁（计划 §4.7；收托盘前也必须先销毁）。
//
// 岛是一个独立、不可信的页面：它只能看到应用经 window.viva 桥交给它的东西和自有 scheme 的资源。
// 本包把已验证的安全与性能约定固化成默认值，业务界面不能绕过：
//
//   - W1（计划 §10.7、§10.9）：岛固定使用 Proxy 非空的独立环境（app.WebProxyDirect），
//     并且只在 Open 期间把 --disable-component-update 追加进 WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS，
//     Open 返回后立刻还原。共享环境里的页面不受益，也不会拿到这个参数。
//   - Ephemeral：cookie、存储与缓存只在内存，关闭即丢。
//   - BridgeMainFrameOnly：内嵌框架拿不到 window.viva。
//   - Navigate 只放行自有 scheme 与 about:blank/srcdoc；NewWindow、Download 不设（即全部拒绝）。
//     局限：被拒绝的 GET 导航，请求仍会发出（headers.go 与台账 A-018），所以它不是出站控制。
//   - 每个响应带内容安全策略与 Permissions-Policy（headers.go）：关掉出站网络、内嵌框架与权限提示。
//   - 每开一个岛，WebView2 都在用户数据目录里留一个 WV2Profile_viva-ephemeral-* 目录且从不删除：
//     Open 之前先清掉没人用的旧目录（profiles.go，R5）；需先用 SetProfileClass 登记窗口 Class。
//   - OnMessage 没有来源信息：页面里任何框架（含内嵌的第三方内容）都可能发消息，
//     调用方必须把收到的文本当作不可信输入校验。
package island

import (
	"errors"
	"fmt"
	"sync"

	"github.com/vivaui/viva/app"
)

// DefaultScheme 是岛默认的自有 scheme 名。
const DefaultScheme = "gzisland"

// Config 描述一个岛。零值不可用：必须给 Serve。
type Config struct {
	// Scheme 是自有 scheme 名；空取 DefaultScheme。
	Scheme string
	// Entry 是入口路径（不含前导斜杠）；空取 index.html。页面地址为 <Scheme>://doc/<Entry>。
	Entry string
	// Serve 按路径提供资源（路径不含主机名与查询串）；ok=false 即 404。
	Serve func(path string) (mime string, data []byte, ok bool)
	// CSP 是每个响应带的内容安全策略；空取 DefaultCSP（脚本只许自有 scheme 的文件）。
	CSP string
	// OnMessage 收到页面 window.viva.postMessage(text) 的文本。无来源信息，按不可信输入处理。
	OnMessage func(text string)
	// OnLoad 报告导航进度（主框架 WebLoadFinished 后页面才可用）。
	OnLoad func(app.WebLoad)
	// OnDenied 在导航被拒绝时调用，供调用方计数与（脱敏后）记日志；可为空。
	OnDenied func(url string, mainFrame bool)
}

// Island 是一个已打开的岛。
type Island struct {
	view *app.WebView
	once sync.Once
}

// webViewConfig 把 Config 装配成 Viva 的页面配置，固化 §包注释里的安全约定。
// 单独成函数，是因为它不需要 WebView2 就能测。
func webViewConfig(cfg Config) (app.WebViewConfig, error) {
	if cfg.Serve == nil {
		return app.WebViewConfig{}, errors.New("island: Serve 不能为空")
	}
	scheme := cfg.Scheme
	if scheme == "" {
		scheme = DefaultScheme
	}
	entry := cfg.Entry
	if entry == "" {
		entry = "index.html"
	}
	return app.WebViewConfig{
		URL:    fmt.Sprintf("%s://%s/%s", scheme, entryHost, entry),
		Scheme: newScheme(scheme, cfg.CSP, cfg.Serve),
		// 独立的代理环境（W1）：direct = 绕过系统代理。岛不碰网络，这里的目的是拿到自己的环境与浏览器进程。
		Proxy:               app.WebProxyDirect,
		Ephemeral:           true,
		BridgeMainFrameOnly: true,
		OnMessage:           cfg.OnMessage,
		OnLoad:              cfg.OnLoad,
		Navigate: func(n app.WebNavigation) bool {
			if allowNavigation(scheme, n.URL) {
				return true
			}
			if cfg.OnDenied != nil {
				cfg.OnDenied(n.URL, n.MainFrame)
			}
			return false
		},
		// NewWindow、Download 不设：窗口与下载一律拒绝。
	}, nil
}

// Open 在窗口线程上打开一个岛。WebHost.Open 同步返回，页面随后异步加载。
// 同一进程里的 Open 被串行化：环境变量是进程级的，W1 只在 Open 期间改它。
func Open(host *app.WebHost, cfg Config) (*Island, error) {
	wc, err := webViewConfig(cfg)
	if err != nil {
		return nil, err
	}
	openMu.Lock()
	defer openMu.Unlock()
	// R5：Viva 的临时 profile 目录关岛后不会被删，开新岛之前先清掉没人用的旧目录（profiles.go）。
	sweepStaleProfiles()
	restore := appendBrowserArgs(componentUpdateOff)
	defer restore()
	view, err := host.Open(wc)
	if err != nil {
		return nil, fmt.Errorf("island: 打开页面失败：%w", err)
	}
	openCount.Add(1)
	return &Island{view: view}, nil
}

// View 返回底层页面，用于 Canvas.NativeView 摆放、PostMessage、Eval 与 Focus。
func (i *Island) View() *app.WebView { return i.view }

// Close 销毁岛；可重复调用。窗口线程调用。
func (i *Island) Close() {
	if i == nil {
		return
	}
	i.once.Do(func() {
		i.view.Close()
		openCount.Add(-1)
	})
}
