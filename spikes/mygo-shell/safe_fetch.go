package main

// 对应 services/import/safe-fetch.ts：采集链路的出站 HTTP。
// SSRF 防护（每跳重定向都校验并钉扎解析出的 IP）、手动代理、空闲超时与大小上限。
//
// 与 TS 版的差别：
//   - 不发 Accept-Encoding（DisableCompression），与 Node http.request 一致，响应体按原始字节计上限。
//   - 字符集解码用 x/text 的 WHATWG 标签表；非法字节的替换字符位置与 TextDecoder 可能不同（已接受，见迁移文档）。
//   - 取消与超时的错误文本沿用中文："已取消"、"请求超时"。

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	whatwg "github.com/nlnwa/whatwg-url/url"
	"golang.org/x/text/encoding/htmlindex"
)

const (
	fetchIdleTimeout = 30 * time.Second
	fetchHTMLMax     = 10 << 20
	fetchJSONMax     = 8 << 20
	fetchMaxRedirect = 5
	fetchDefaultUA   = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36 GuiZhi/0.3"
	fetchHTMLAccept  = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"
	fetchJSONAccept  = "application/json,text/plain;q=0.9,*/*;q=0.8"
)

type safeRequestOptions struct {
	UserAgent string
	Accept    string
	Referer   string
	Cookie    string // 原样写入 Cookie 头（NGA guestJs 握手）
	// 除 2xx 外也交给 consume 读 body 的状态码（NGA 的 guestJs 挑战是 403）
	AcceptStatuses []int
	// AcceptEncoding 非空时随请求发出（调用方负责解压）；NoLanguage 不发 Accept-Language（网页采集与渲染窗口同一身份）。
	AcceptEncoding string
	NoLanguage     bool
}

// safeFetcher 持有可替换的解析器与拨号器，生产用 newSafeFetcher，测试注入。
type safeFetcher struct {
	proxy  *netProxyState
	lookup lookupFunc
	// dialOverride 仅测试使用：替代真实拨号（忽略钉扎地址）。
	dialOverride func(ctx context.Context, network, addr string) (net.Conn, error)
	idle         time.Duration
}

func newSafeFetcher() *safeFetcher {
	return &safeFetcher{proxy: activeNetProxy, lookup: systemLookup, idle: fetchIdleTimeout}
}

var defaultSafeFetcher = newSafeFetcher()

type fetchHTMLResult struct {
	FinalURL    string
	HTML        string
	ContentType string
}

type fetchRawTextResult struct {
	Status      int
	Text        string
	ContentType string
	FinalURL    string
	SetCookies  []string // Set-Cookie 拆成的 name=value
}

// ---- 目标校验 ----

// assertSafeTarget 对应 assertSafeTarget：校验目标并返回要钉扎的 IP（走代理时为 nil）。
// 走代理时不解析域名也不钉扎（域名由代理解析），但 URL 里直接写的字面私网 IP 照常拦下。
func (f *safeFetcher) assertSafeTarget(ctx context.Context, u *whatwg.Url, viaProxy bool) (*resolvedAddress, error) {
	if p := u.Protocol(); p != "http:" && p != "https:" {
		return nil, fmt.Errorf("不支持的协议: %s", p)
	}
	host := strings.ToLower(u.Hostname())
	if isBlockedHostname(host) {
		return nil, errors.New("不允许访问本地网络地址")
	}
	literal := host
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		literal = host[1 : len(host)-1]
	}
	_, fam := parseIPLiteral(literal)
	if viaProxy {
		if fam != 0 && isPrivateAddress(literal) {
			return nil, errors.New("不允许访问内网地址")
		}
		return nil, nil
	}
	// fake-ip（198.18/15）只在确实存在代理配置时才放行；没有代理却解析到这个段是真实内网地址
	addr, err := resolvePublicAddress(ctx, literal, resolveOptions{AllowProxyCompatibilityAddress: f.proxy.HasAnyProxyConfigured()}, f.lookup)
	if err != nil {
		msg := err.Error()
		switch {
		case strings.Contains(msg, "internal network") || strings.Contains(msg, "local network"):
			return nil, errors.New("不允许访问内网地址")
		case strings.Contains(msg, "resolve"):
			return nil, fmt.Errorf("无法解析域名：%s", literal)
		}
		return nil, err
	}
	return &addr, nil
}

// ---- 单次请求 ----

type idleConn struct {
	net.Conn
	d time.Duration
}

func (c idleConn) Read(p []byte) (int, error) {
	_ = c.Conn.SetReadDeadline(time.Now().Add(c.d))
	return c.Conn.Read(p)
}

func (c idleConn) Write(p []byte) (int, error) {
	_ = c.Conn.SetWriteDeadline(time.Now().Add(c.d))
	return c.Conn.Write(p)
}

func toNetURL(u *whatwg.Url) (*url.URL, error) { return url.Parse(u.Href(true)) }

func (f *safeFetcher) transport(pinned *resolvedAddress, viaProxy bool) *http.Transport {
	dialer := &net.Dialer{Timeout: f.idle}
	return &http.Transport{
		DisableKeepAlives:   true,
		DisableCompression:  true,
		TLSHandshakeTimeout: f.idle,
		Proxy: func(r *http.Request) (*url.URL, error) {
			if !viaProxy {
				return nil, nil
			}
			return f.proxy.ProxyFor(r.URL), nil
		},
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if pinned != nil {
				// 钉扎到刚校验过的 IP，堵住「校验后 DNS 再变」的 rebinding 窗口；SNI 与 Host 头仍用原域名
				_, port, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}
				addr = net.JoinHostPort(pinned.Address, port)
			}
			dial := dialer.DialContext
			if f.dialOverride != nil {
				dial = f.dialOverride
			}
			c, err := dial(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			return idleConn{Conn: c, d: f.idle}, nil
		},
	}
}

func mapFetchError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return errors.New("已取消")
	}
	var ne net.Error
	if errors.Is(err, os.ErrDeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
		return errors.New("请求超时")
	}
	return err
}

// openRequest 发起单次 GET，响应头到达即返回；重定向交给调用方。
func (f *safeFetcher) openRequest(ctx context.Context, target *whatwg.Url, pinned *resolvedAddress, viaProxy bool, o safeRequestOptions) (*http.Response, error) {
	nu, err := toNetURL(target)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, nu.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Host = target.Host()
	ua, accept := o.UserAgent, o.Accept
	if ua == "" {
		ua = fetchDefaultUA
	}
	if accept == "" {
		accept = fetchHTMLAccept
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", accept)
	if !o.NoLanguage {
		req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	}
	if o.AcceptEncoding != "" {
		req.Header.Set("Accept-Encoding", o.AcceptEncoding)
	}
	if o.Referer != "" {
		req.Header.Set("Referer", o.Referer)
	}
	if o.Cookie != "" {
		req.Header.Set("Cookie", o.Cookie)
	}
	client := &http.Client{
		Transport:     f.transport(pinned, viaProxy),
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, mapFetchError(ctx, err)
	}
	return resp, nil
}

// safeRequest 对应 requestFollowingRedirects：跟随重定向直到拿到最终响应，每一跳都重新校验。
func safeRequest[T any](ctx context.Context, f *safeFetcher, rawURL string, o safeRequestOptions,
	consume func(resp *http.Response, finalURL string, status int) (T, error)) (T, error) {
	var zero T
	current := rawURL
	for redirect := 0; redirect <= fetchMaxRedirect; redirect++ {
		parsed, err := whatwg.Parse(current)
		if err != nil {
			return zero, errors.New("Invalid URL")
		}
		nu, err := toNetURL(parsed)
		if err != nil {
			return zero, errors.New("Invalid URL")
		}
		viaProxy := f.proxy.ProxyFor(nu) != nil
		pinned, err := f.assertSafeTarget(ctx, parsed, viaProxy)
		if err != nil {
			return zero, err
		}
		resp, err := f.openRequest(ctx, parsed, pinned, viaProxy, o)
		if err != nil {
			return zero, err
		}
		status := resp.StatusCode
		if loc := resp.Header.Get("Location"); status >= 300 && status < 400 && loc != "" {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
			resp.Body.Close()
			next, err := whatwg.ParseRef(current, loc)
			if err != nil {
				return zero, errors.New("Invalid URL")
			}
			current = next.Href(false)
			continue
		}
		accepted := status >= 200 && status < 300
		for _, s := range o.AcceptStatuses {
			if s == status {
				accepted = true
			}
		}
		if !accepted {
			resp.Body.Close()
			return zero, fmt.Errorf("HTTP %d", status)
		}
		out, err := consume(resp, current, status)
		resp.Body.Close()
		if err != nil {
			return zero, mapFetchError(ctx, err)
		}
		return out, nil
	}
	return zero, errors.New("重定向次数过多")
}

// readBodyLimited 读取响应体；超过上限即中止。
func readBodyLimited(r io.Reader, max int64, overflow string) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, errors.New(overflow)
	}
	return b, nil
}

// ---- 公开入口 ----

var htmlTypeRe = regexp.MustCompile(`(?i)text/html|application/xhtml`)
var jsonTypeRe = regexp.MustCompile(`(?i)json|text/plain`)

func (f *safeFetcher) fetchHTML(ctx context.Context, rawURL string, o safeRequestOptions) (fetchHTMLResult, error) {
	return safeRequest(ctx, f, rawURL, o, func(resp *http.Response, finalURL string, _ int) (fetchHTMLResult, error) {
		ct := resp.Header.Get("Content-Type")
		if ct != "" && !htmlTypeRe.MatchString(ct) {
			return fetchHTMLResult{}, fmt.Errorf("不是网页内容: %s", strings.SplitN(ct, ";", 2)[0])
		}
		body, err := readBodyLimited(resp.Body, fetchHTMLMax, "页面超过大小上限")
		if err != nil {
			return fetchHTMLResult{}, err
		}
		return fetchHTMLResult{FinalURL: finalURL, HTML: decodeTextBody(body, ct, true), ContentType: ct}, nil
	})
}

// fetchRawText 对应 fetchRawText：任意文本响应（NGA lite=js 既不是纯 JSON 也不是 HTML，常为 GBK）。
func (f *safeFetcher) fetchRawText(ctx context.Context, rawURL string, o safeRequestOptions, maxBytes int64) (fetchRawTextResult, error) {
	if o.Accept == "" {
		o.Accept = "*/*"
	}
	if maxBytes <= 0 {
		maxBytes = fetchJSONMax
	}
	return safeRequest(ctx, f, rawURL, o, func(resp *http.Response, finalURL string, status int) (fetchRawTextResult, error) {
		ct := resp.Header.Get("Content-Type")
		body, err := readBodyLimited(resp.Body, maxBytes, "接口响应超过大小上限")
		if err != nil {
			return fetchRawTextResult{}, err
		}
		var cookies []string
		for _, line := range resp.Header.Values("Set-Cookie") {
			part := strings.TrimSpace(strings.SplitN(line, ";", 2)[0])
			if part != "" && strings.Contains(part, "=") {
				cookies = append(cookies, part)
			}
		}
		return fetchRawTextResult{Status: status, Text: decodeTextBody(body, ct, false), ContentType: ct, FinalURL: finalURL, SetCookies: cookies}, nil
	})
}

// fetchJSON 对应 fetchJSON：论坛采集走平台只读 API；返回原始 JSON 字节供调用方解码。
func (f *safeFetcher) fetchJSON(ctx context.Context, rawURL string, o safeRequestOptions) (json.RawMessage, error) {
	if o.Accept == "" {
		o.Accept = fetchJSONAccept
	}
	return safeRequest(ctx, f, rawURL, o, func(resp *http.Response, _ string, _ int) (json.RawMessage, error) {
		ct := resp.Header.Get("Content-Type")
		// 被登录墙或 WAF 拦下时返回的是 HTML，早点报错比让解析抛乱码强
		if ct != "" && !jsonTypeRe.MatchString(ct) {
			return nil, fmt.Errorf("接口未返回 JSON: %s", strings.SplitN(ct, ";", 2)[0])
		}
		body, err := readBodyLimited(resp.Body, fetchJSONMax, "接口响应超过大小上限")
		if err != nil {
			return nil, err
		}
		if !json.Valid(body) {
			return nil, errors.New("接口响应不是合法的 JSON")
		}
		return body, nil
	})
}

// resolveRedirectURL 对应 resolveRedirectUrl：只解析重定向落点，响应头到达即释放正文。
func (f *safeFetcher) resolveRedirectURL(ctx context.Context, rawURL string) (string, error) {
	return safeRequest(ctx, f, rawURL, safeRequestOptions{}, func(_ *http.Response, finalURL string, _ int) (string, error) {
		return finalURL, nil
	})
}

// downloadToTempFile 对应 downloadToTempFile：流式下载到独立临时目录。调用方负责删除返回的目录（失败时已自行清理）。
func (f *safeFetcher) downloadToTempFile(ctx context.Context, rawURL, fileName string, maxBytes int64, o safeRequestOptions) (dir, filePath string, err error) {
	var rnd [4]byte
	_, _ = rand.Read(rnd[:])
	dir = filepath.Join(os.TempDir(), "guizhi-download-"+hex.EncodeToString(rnd[:]))
	filePath = filepath.Join(dir, fileName)
	if err = os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	if o.Accept == "" {
		o.Accept = "*/*"
	}
	_, err = safeRequest(ctx, f, rawURL, o, func(resp *http.Response, _ string, _ int) (struct{}, error) {
		if regexp.MustCompile(`(?i)text/html`).MatchString(resp.Header.Get("Content-Type")) {
			return struct{}{}, errors.New("下载地址返回的是网页而非媒体文件（可能已失效）")
		}
		out, err := os.Create(filePath)
		if err != nil {
			return struct{}{}, err
		}
		n, err := io.Copy(out, io.LimitReader(resp.Body, maxBytes+1))
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err == nil && n > maxBytes {
			err = errors.New("文件超过大小上限")
		}
		return struct{}{}, err
	})
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", "", err
	}
	return dir, filePath, nil
}

// ---- 解码 ----

var (
	charsetHeaderRe = regexp.MustCompile(`(?i)charset=([\w-]+)`)
	ngaEncodeRe     = regexp.MustCompile(`(?i)"encode"\s*:\s*"(gbk|gb2312|utf-8|utf8)"`)
	metaCharsetRe   = regexp.MustCompile(`(?i)<meta[^>]+charset=["']?([\w-]+)`)
)

// decodeTextBody 对应 decodeTextBody：Content-Type → NGA 的 "encode" 字段 → meta charset → UTF-8。
func decodeTextBody(body []byte, contentType string, peekMeta bool) string {
	charset := ""
	if m := charsetHeaderRe.FindStringSubmatch(contentType); m != nil {
		charset = m[1]
	}
	if charset == "" {
		peek := body
		if len(peek) > 4096 {
			peek = peek[:4096]
		}
		// latin1 逐字节映射：正则只关心 ASCII，直接按字节匹配
		if m := ngaEncodeRe.FindSubmatch(peek); m != nil {
			charset = string(m[1])
		}
	}
	if charset == "" && peekMeta {
		head := utf16Slice(lossyUTF8(body), 2048)
		if m := metaCharsetRe.FindStringSubmatch(head); m != nil {
			charset = m[1]
		}
	}
	if charset == "" {
		charset = "utf-8"
	}
	charset = strings.ToLower(charset)
	if charset == "utf-8" || charset == "utf8" {
		return lossyUTF8(body)
	}
	if charset == "gb2312" {
		charset = "gbk"
	}
	enc, err := htmlindex.Get(strings.TrimSpace(charset))
	if err != nil {
		return lossyUTF8(body)
	}
	enc = whatwgDecoderFor(enc)
	if name, _ := htmlindex.Name(enc); name == "replacement" {
		return lossyUTF8(body)
	}
	out, err := enc.NewDecoder().Bytes(body)
	if err != nil {
		return lossyUTF8(body)
	}
	return string(out)
}
