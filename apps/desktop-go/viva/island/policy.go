package island

import (
	"net/url"
	"strings"
)

// entryHost 是岛页面地址里固定的主机名：scheme://doc/<path>。
// Windows 上 Viva 报给 Navigate 的主框架地址就是这个形式（计划 §10.6 实测："gzisland://doc/index.html"）。
const entryHost = "doc"

// allowNavigation 决定岛里一次导航是否放行：只放行自己的 scheme（主机必须是 entryHost）与内层文档的
// about:blank / about:srcdoc，其余一律拒绝（包括 http(s)、data、blob、javascript、file）。
//
// 不能用「地址里包含 scheme 字样」判断：`https://evil.example/?next=gzisland://doc/` 也包含它。
// 解析失败按拒绝处理（fail closed）。
//
// Only the island's own scheme (host must be "doc") and about:blank / about:srcdoc for inner documents
// are allowed; everything else is refused. Substring matching would let a query string spoof the scheme.
func allowNavigation(scheme, rawURL string) bool {
	if scheme == "" {
		return false
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "about":
		// about:blank 与 about:srcdoc 是 iframe 内层文档的地址；其余 about: 页面（如 about:net-internals）不放行。
		return rawURL == "about:blank" || rawURL == "about:srcdoc"
	case strings.ToLower(scheme):
		// 主机名必须是 entryHost，且不带用户信息与端口（scheme://doc@evil/、scheme://doc:80/ 一律拒绝）。
		return u.Host == entryHost && u.User == nil && u.Port() == ""
	}
	return false
}
