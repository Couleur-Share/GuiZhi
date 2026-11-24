package imports

import "testing"

// 对应渲染层 packages/shared/utils/web-scope.ts 的 webScope：只有预览与按钮开关，真正的校验在主进程。

func TestDocSiteScopeValid(t *testing.T) {
	cases := []struct{ name, entry, dir, want string }{
		{"入口所在目录", "https://docs.example.com/guide/intro.html", "", "https://docs.example.com/guide/"},
		{"主机与默认端口规整", "https://Docs.Example.com:443/guide/intro.html", "", "https://docs.example.com/guide/"},
		{"http 默认端口", "http://a.com:80/x/y", "", "http://a.com/x/"},
		{"非默认端口保留", "http://a.com:8080/x/y", "", "http://a.com:8080/x/"},
		{"只有域名", "https://a.com", "", "https://a.com/"},
		{"入口本身是目录", "https://a.com/guide/", "", "https://a.com/guide/"},
		{"显式目录", "https://a.com/guide/", "/guide/api", "https://a.com/guide/api/"},
		{"目录补尾斜杠", "https://a.com/", "/docs", "https://a.com/docs/"},
		{"粘贴的首尾空白与换行", "  https://a.com/x/y \n", "", "https://a.com/x/"},
		{"输入内的换行被丢弃", "https://a.com/x/\ny.html", "", "https://a.com/x/"},
		{"入口里的点段被折叠", "https://a.com/a/b/../c", "", "https://a.com/a/"},
		{"目录取解码后的路径", "https://a.com/", "/%E6%8C%87%E5%8D%97", "https://a.com/\u6307\u5357/"},
		{"目录是中文原文", "https://a.com/", "/\u6307\u5357", "https://a.com/\u6307\u5357/"},
		{"查询串不参与目录", "https://a.com/x/y.html?q=1#top", "", "https://a.com/x/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DocSiteScope(tc.entry, tc.dir)
			if err != nil || got != tc.want {
				t.Fatalf("DocSiteScope(%q, %q) = %q, %v，期望 %q", tc.entry, tc.dir, got, err, tc.want)
			}
		})
	}
}

func TestDocSiteScopeInvalid(t *testing.T) {
	cases := []struct{ name, entry, dir string }{
		{"空入口", "", ""},
		{"不是网址", "not a url", ""},
		{"非 http(s)", "ftp://a.com/x/", ""},
		{"javascript 伪协议", "javascript:alert(1)", ""},
		{"带账号密码", "https://u:p@a.com/x/", ""},
		{"只有用户名", "https://u@a.com/x/", ""},
		{"没有主机", "https:///x/", ""},
		{"目录不以斜杠开头", "https://a.com/", "guide"},
		{"目录带查询", "https://a.com/", "/a?x=1"},
		{"目录带片段", "https://a.com/", "/a#x"},
		{"目录含编码斜杠", "https://a.com/", "/a/%2fb"},
		{"目录含编码反斜杠", "https://a.com/", "/a/%5Cb"},
		{"目录含点段", "https://a.com/", "/a/./b"},
		{"目录含上级段", "https://a.com/", "/a/../b"},
		{"目录含编码的点", "https://a.com/", "/a/%2e%2e/b"},
		{"目录含反斜杠", "https://a.com/", `/a\b`},
		{"目录转义不合法", "https://a.com/", "/a/%zz"},
		{"目录解码后不是 UTF-8", "https://a.com/", "/a/%ff"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := DocSiteScope(tc.entry, tc.dir); err == nil {
				t.Fatalf("DocSiteScope(%q, %q) = %q，期望报错", tc.entry, tc.dir, got)
			}
		})
	}
}
