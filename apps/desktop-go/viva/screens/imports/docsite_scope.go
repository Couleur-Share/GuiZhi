package imports

import (
	"errors"
	"net/url"
	"strings"
	"unicode/utf8"
)

// 采集文档站的「实际范围」预览（渲染层 packages/shared/utils/web-scope.ts 的 webScope）：
// 同源、按目录边界匹配，界面把 origin + 目录 显示在「实际范围：」后面，范围无效时开始按钮不可点。
//
// 这只是**预览与按钮的开关**：入口网址、目录、数值上限与「入口是否在目录内」都由主进程的 crawl:create 处理器再校验一遍，
// 原因原样带回（SSRF、robots 与同源判定也在那一侧）。所以这里宁可与 WHATWG URL 有细微差别，也不能把处理器会接受的输入挡在门外：
// 只做同样的拒绝（非 http(s)、带账号密码、没有主机、目录含编码路径分隔符或 ./.. 段），不做主机名的 IDNA 转写。

var (
	errScopeURL       = errors.New("请输入不含账号密码的 HTTP(S) 网址")
	errScopeDirectory = errors.New("目录必须为 / 开头的路径")
	errScopeAmbiguous = errors.New("目录包含含糊的编码或路径")
	errScopeSeparator = errors.New("目录包含编码路径分隔符")
)

// DocSiteScope 求入口网址与目录范围对应的实际范围（origin + 目录）。directory 为空取入口所在的目录；
// 无效返回错误（界面只据此显示「请输入有效网址和目录」，错误文本留给日志与测试）。
func DocSiteScope(entry, directory string) (string, error) {
	u, err := parseWebURL(entry)
	if err != nil {
		return "", err
	}
	path := directory
	if path == "" {
		whole := pathnameOf(u)
		path = whole[:strings.LastIndex(whole, "/")+1]
	}
	if !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "?#") {
		return "", errScopeDirectory
	}
	clean, err := safeScopePath(path)
	if err != nil {
		return "", err
	}
	if !strings.HasSuffix(clean, "/") {
		clean += "/"
	}
	return originOf(u) + clean, nil
}

// parseWebURL 对应 canonicalWebUrl 的校验部分：http(s)、没有账号密码、有主机。
// WHATWG 解析前会去掉首尾的控制字符与空格、并丢弃输入里的制表与换行，这里照做（粘贴进来的网址常带尾随空格）。
func parseWebURL(raw string) (*url.URL, error) {
	raw = strings.TrimFunc(raw, func(r rune) bool { return r <= 0x20 })
	raw = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, raw)
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" {
		return nil, errScopeURL
	}
	scheme := strings.ToLower(u.Scheme)
	if (scheme != "http" && scheme != "https") || u.User != nil || u.Hostname() == "" {
		return nil, errScopeURL
	}
	u.Scheme = scheme
	return u, nil
}

// originOf 是 scheme://host[:port]：主机小写，默认端口省略（URL.origin）。
func originOf(u *url.URL) string {
	host := strings.ToLower(u.Host)
	if port := u.Port(); (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		host = strings.TrimSuffix(host, ":"+port)
	}
	return u.Scheme + "://" + host
}

// pathnameOf 是 URL.pathname：百分号编码形式，并按 WHATWG 去掉 . 与 .. 段（含 %2e 写法）；空路径是 /。
func pathnameOf(u *url.URL) string {
	p := u.EscapedPath()
	if p == "" {
		return "/"
	}
	segments := strings.Split(p, "/")[1:]
	out := make([]string, 0, len(segments))
	for i, seg := range segments {
		last := i == len(segments)-1
		switch strings.ToLower(seg) {
		case ".", "%2e":
			if last {
				out = append(out, "")
			}
		case "..", ".%2e", "%2e.", "%2e%2e":
			if len(out) > 0 {
				out = out[:len(out)-1]
			}
			if last {
				out = append(out, "")
			}
		default:
			out = append(out, seg)
		}
	}
	return "/" + strings.Join(out, "/")
}

// safeScopePath 对应 safePath：拒绝编码的路径分隔符、解不开的转义、反斜杠、二次编码的点与 . / .. 段；返回解码后的路径。
func safeScopePath(path string) (string, error) {
	lower := strings.ToLower(path)
	if strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") {
		return "", errScopeSeparator
	}
	decoded, err := url.PathUnescape(path)
	if err != nil || !utf8.ValidString(decoded) {
		return "", errScopeAmbiguous
	}
	low := strings.ToLower(decoded)
	if strings.Contains(decoded, `\`) || strings.Contains(low, "%2f") || strings.Contains(low, "%5c") || strings.Contains(low, "%2e") {
		return "", errScopeAmbiguous
	}
	for _, seg := range strings.Split(decoded, "/") {
		if seg == "." || seg == ".." {
			return "", errScopeAmbiguous
		}
	}
	return decoded, nil
}
