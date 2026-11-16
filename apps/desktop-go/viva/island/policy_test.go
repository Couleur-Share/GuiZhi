package island

import "testing"

func TestAllowNavigation(t *testing.T) {
	const scheme = "gzisland"
	cases := []struct {
		url  string
		want bool
		why  string
	}{
		{"gzisland://doc/index.html", true, "自有 scheme 的入口页（Windows 上实测的主框架地址形式）"},
		{"gzisland://doc/assets/a.png", true, "自有 scheme 的资源"},
		{"GZISLAND://doc/index.html", true, "scheme 不区分大小写"},
		{"about:blank", true, "iframe 内层文档"},
		{"about:srcdoc", true, "iframe srcdoc"},

		{"https://example.com/", false, "外部 https"},
		{"http://gzisland.localhost/index.html", false, "伪装成自有 scheme 的 http 主机"},
		{"https://evil.example/?next=gzisland://doc/index.html", false, "查询串里带 scheme 字样不能放行"},
		{"https://gzisland://doc/", false, "路径里带 scheme 字样"},
		{"gzisland://evil.example/index.html", false, "自有 scheme 但主机不是 doc"},
		{"gzisland://doc@evil.example/index.html", false, "用户信息伪装主机"},
		{"gzisland://doc:8080/index.html", false, "带端口"},
		{"gzisland:doc/index.html", false, "不透明形式（没有主机）"},
		{"about:net-internals", false, "其他 about 页面"},
		{"about:blank#x", false, "about 带片段按拒绝处理"},
		{"data:text/html,<h1>x</h1>", false, "data"},
		{"blob:https://example.com/uuid", false, "blob"},
		{"javascript:alert(1)", false, "javascript"},
		{"file:///C:/Windows/win.ini", false, "file"},
		{"", false, "空地址"},
		{"://bad", false, "解析失败按拒绝"},
		{"gzisland://doc/%zz", false, "非法转义解析失败按拒绝"},
	}
	for _, c := range cases {
		if got := allowNavigation(scheme, c.url); got != c.want {
			t.Errorf("allowNavigation(%q) = %v，期望 %v（%s）", c.url, got, c.want, c.why)
		}
	}
	if allowNavigation("", "gzisland://doc/index.html") {
		t.Error("scheme 为空时必须拒绝")
	}
}
