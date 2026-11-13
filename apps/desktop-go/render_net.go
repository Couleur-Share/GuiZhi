package main

// 对应 web-network.ts 的 webNetworkRequest：渲染窗口所有子请求的唯一出口。
// 每次请求单独做 SSRF 校验并钉扎解析出的 IP（重定向由浏览器再次发起，重新走这里）；代理失败不降级直连。
//
// 与 TS 版的差别：Accept-Encoding 只声明 gzip、deflate（Go 标准库无 brotli）；不做连接池复用。

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	whatwg "github.com/nlnwa/whatwg-url/url"
)

const webSubRequestBodyLimit = 1 << 20

type webSubRequest struct {
	URL     string
	Method  string
	Headers map[string]string
	Body    []byte
}

var reWebForbiddenRequestHeader = regexp.MustCompile(`(?i)^(host|connection|content-length|proxy-.*|authorization|upgrade|transfer-encoding)$`)

func (f *safeFetcher) webSubRequest(ctx context.Context, in webSubRequest) (webHop, error) {
	rawURL, err := canonicalWebURL(in.URL)
	if err != nil {
		return webHop{}, err
	}
	method := in.Method
	if method == "" {
		method = http.MethodGet
	}
	switch method {
	case "GET", "HEAD", "POST", "OPTIONS":
	default:
		return webHop{}, errors.New("网页请求方法不受支持")
	}
	if len(in.Body) > webSubRequestBodyLimit {
		return webHop{}, errors.New("网页请求体超过 1 MiB")
	}
	parsed, nu, err := parseWebURL(rawURL)
	if err != nil {
		return webHop{}, err
	}
	viaProxy := f.proxy.ProxyFor(nu) != nil
	pinned, err := f.assertSafeTarget(ctx, parsed, viaProxy)
	if err != nil {
		return webHop{}, err
	}
	req, err := http.NewRequestWithContext(ctx, method, nu.String(), bytes.NewReader(in.Body))
	if err != nil {
		return webHop{}, err
	}
	if len(in.Body) == 0 {
		req.Body, req.ContentLength = http.NoBody, 0
	}
	req.Host = parsed.Host()
	for k, v := range in.Headers {
		if reWebForbiddenRequestHeader.MatchString(k) || strings.ContainsAny(v, "\r\n") {
			continue
		}
		req.Header.Set(k, v)
	}
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	client := &http.Client{
		Transport:     f.transport(pinned, viaProxy, nu),
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		return webHop{}, mapFetchError(ctx, unwrapURLError(err))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, webResponseLimit+1))
	if err != nil {
		return webHop{}, mapFetchError(ctx, err)
	}
	if len(raw) > webResponseLimit {
		return webHop{}, errors.New("网页响应超过 10 MiB")
	}
	data, err := decodeWebContentEncoding(raw, resp.Header.Get("Content-Encoding"))
	if err != nil {
		return webHop{}, err
	}
	h := map[string]string{}
	for k, vs := range resp.Header {
		switch lk := strings.ToLower(k); lk {
		case "content-encoding", "content-length", "transfer-encoding", "connection", "alt-svc":
		default:
			h[lk] = strings.Join(vs, "\n")
		}
	}
	return webHop{Status: resp.StatusCode, Headers: h, Body: data}, nil
}

// parseWebURL 解析为 WHATWG 地址与 net/url 地址（后者用于代理判定与请求）。
func parseWebURL(raw string) (*whatwg.Url, *url.URL, error) {
	parsed, err := whatwg.Parse(raw)
	if err != nil {
		return nil, nil, errors.New("Invalid URL")
	}
	nu, err := toNetURL(parsed)
	if err != nil {
		return nil, nil, errors.New("Invalid URL")
	}
	return parsed, nu, nil
}

func unwrapURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}
