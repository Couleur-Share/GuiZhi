package main

// 对应 shared/utils/web-scope.ts：目录范围（同源 + 目录前缀）与链接过滤。

import (
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	whatwg "github.com/nlnwa/whatwg-url/url"
)

type webScope struct {
	Origin    string `json:"origin"`
	Directory string `json:"directory"`
}

var (
	reEncodedSep   = regexp.MustCompile(`(?i)%2f|%5c`)
	reAmbiguousSeg = regexp.MustCompile(`(?i)\\|%2f|%5c|%2e`)
	reNonPageExt   = regexp.MustCompile(`(?i)\.(?:pdf|zip|gz|tar|exe|dmg|msi|mp[34]|png|jpe?g|gif|webp|svg|woff2?|css|js|xml)$`)
)

// decodeURIComponentJS 对应 decodeURIComponent：畸形转义或非法 UTF-8 报错（URIError）。
func decodeURIComponentJS(s string) (string, error) {
	if !strings.Contains(s, "%") {
		return s, nil
	}
	buf := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '%' {
			buf = append(buf, c)
			continue
		}
		if i+2 >= len(s) {
			return "", errors.New("URI malformed")
		}
		hi, ok1 := hexDigit(s[i+1])
		lo, ok2 := hexDigit(s[i+2])
		if !ok1 || !ok2 {
			return "", errors.New("URI malformed")
		}
		buf = append(buf, hi<<4|lo)
		i += 2
	}
	if !utf8.Valid(buf) {
		return "", errors.New("URI malformed")
	}
	return string(buf), nil
}

func hexDigit(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

func safeWebPath(path string) (string, error) {
	if reEncodedSep.MatchString(path) {
		return "", errors.New("目录包含编码路径分隔符")
	}
	decoded, err := decodeURIComponentJS(path)
	if err != nil {
		return "", err
	}
	if reAmbiguousSeg.MatchString(decoded) {
		return "", errors.New("目录包含含糊的编码或路径")
	}
	for _, p := range strings.Split(decoded, "/") {
		if p == ".." || p == "." {
			return "", errors.New("目录包含含糊的编码或路径")
		}
	}
	return decoded, nil
}

// newWebScope 对应 webScope(entry, directory?)。
func newWebScope(entry string, directory *string) (webScope, error) {
	canon, err := canonicalWebURL(entry)
	if err != nil {
		return webScope{}, err
	}
	u, err := whatwg.Parse(canon)
	if err != nil {
		return webScope{}, errors.New("Invalid URL")
	}
	pathname := u.Pathname()
	dir := pathname[:strings.LastIndex(pathname, "/")+1]
	if directory != nil {
		dir = *directory
	}
	if !strings.HasPrefix(dir, "/") || strings.ContainsAny(dir, "?#") {
		return webScope{}, errors.New("目录必须为 / 开头的路径")
	}
	clean, err := safeWebPath(dir)
	if err != nil {
		return webScope{}, err
	}
	if !strings.HasSuffix(clean, "/") {
		clean += "/"
	}
	return webScope{Origin: webOrigin(canon), Directory: clean}, nil
}

// inWebScope 对应 inWebScope：任何解析失败都视为不在范围内。
func inWebScope(raw string, scope webScope) bool {
	canon, err := canonicalWebURL(raw)
	if err != nil {
		return false
	}
	u, err := whatwg.Parse(canon)
	if err != nil {
		return false
	}
	path, err := safeWebPath(u.Pathname())
	if err != nil {
		return false
	}
	return webOrigin(canon) == scope.Origin && (strings.HasPrefix(path, scope.Directory) || path == scope.Directory[:len(scope.Directory)-1])
}

// isWebPageLink 对应 isWebPageLink：排除常见的非网页资源后缀。
func isWebPageLink(raw string) bool {
	u, err := whatwg.Parse(raw)
	if err != nil {
		return false
	}
	return !reNonPageExt.MatchString(u.Pathname())
}
