package island

import "github.com/vivaui/viva/app"

// 岛的响应头：自有 scheme 的每个响应（含 404）都带这些头，是不可信内容的第二道墙
// （第一道是应用侧对内容的清理：不放行脚本、meta refresh、内嵌框架）。
//
// 依据是计划 §4.7 的安全探针（viva/cmd/islandprobe，结果记在 docs/agents/viva-ui-development.md §8.3）：
//   - 不带策略时，岛里的页面可以向任意地址发 fetch / XHR / 图片 / 样式 / 脚本 / WebSocket / 内嵌框架 / sendBeacon 请求，
//     全部真的发出了网络请求；内容安全策略能把这些全部关掉。
//   - 不带策略时，页面请求定位会让 WebView2 弹出默认的权限气泡（探针的窗口监视看到一个可见的 Chromium 弹窗，
//     请求一直挂起），摄像头等的查询状态是 prompt；带上 Permissions-Policy 后查询是 denied、请求立刻失败、没有弹窗。
//     实测只覆盖定位与摄像头的状态查询和定位请求；麦克风、MIDI 等其余特性列在策略里，没有逐项请求验证。
//     通知不归 Permissions-Policy 管（查询仍是 prompt），但 requestPermission() 本来就直接返回 denied、没有提示。
//   - 局限（A-018）：主框架与内嵌框架的 GET 导航被 Navigate 拒绝之后，请求仍然会发出去（探针里回环服务收到了
//     Sec-Fetch-Mode: navigate 的请求，页面没有被替换）。内容安全策略管不到导航，所以岛里的内容不能带 <meta refresh>、
//     脚本与内嵌框架——那是应用侧清理的职责，不能指望 Navigate。
//
// Island responses carry a closed CSP and a Permissions-Policy. A refused GET navigation still sends the
// request (measured), which CSP cannot stop, so untrusted content must be sanitized before it reaches an island.

// DefaultCSP 是岛的默认内容安全策略：脚本只允许自有 scheme 的文件（不允许内联与 eval），出站网络与内嵌框架全部关闭；
// 样式允许内联（界面大量使用 style 属性；样式里的外链会被 img-src / font-src / style-src 拦下）。
// 需要别的策略（例如探针页面是内联脚本）经 Config.CSP 覆盖。
const DefaultCSP = "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob:; font-src 'self' data:; media-src 'self' data: blob:; connect-src 'self'; " +
	"frame-src about:; object-src 'none'; base-uri 'none'; form-action 'none'"

// DefaultPermissionsPolicy 关掉会弹权限提示或接触硬件的特性。剪贴板写入、全屏与自动播放不在其中：
// 阅读视图的复制按钮与媒体播放需要它们（clipboard-read 关闭）。
const DefaultPermissionsPolicy = "accelerometer=(), ambient-light-sensor=(), battery=(), bluetooth=(), camera=(), " +
	"clipboard-read=(), display-capture=(), geolocation=(), gyroscope=(), hid=(), idle-detection=(), magnetometer=(), " +
	"microphone=(), midi=(), payment=(), publickey-credentials-get=(), screen-wake-lock=(), serial=(), usb=(), " +
	"xr-spatial-tracking=()"

// responseHeaders 返回一次响应要带的加固头。每次新建一份，调用方可以随意改而不影响下一次响应。
func responseHeaders(csp string) map[string]string {
	if csp == "" {
		csp = DefaultCSP
	}
	return map[string]string{
		"Content-Security-Policy": csp,
		"Permissions-Policy":      DefaultPermissionsPolicy,
		"Referrer-Policy":         "no-referrer",
		"X-Content-Type-Options":  "nosniff",
	}
}

// newScheme 装配岛的自有 scheme。Respond 优先于 Serve，所以实际走的是带头的路径；Serve 保留给不支持 Respond 的平台。
func newScheme(name, csp string, serve func(path string) (mime string, data []byte, ok bool)) *app.WebScheme {
	return &app.WebScheme{
		Name:  name,
		Serve: serve,
		Respond: func(request app.WebRequest) (app.WebResponse, bool) {
			mime, data, ok := serve(request.Path)
			if !ok {
				return app.WebResponse{Status: 404, MIME: "text/plain; charset=utf-8", Data: []byte("not found"), Headers: responseHeaders(csp)}, false
			}
			return app.WebResponse{MIME: mime, Data: data, Headers: responseHeaders(csp)}, true
		},
	}
}
