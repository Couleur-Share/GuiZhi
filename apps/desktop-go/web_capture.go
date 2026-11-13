package main

// 对应 services/web-capture/{web-capture,web-electron-capture(静态路由部分),web-network,web-request-identity,
// web-node-extractor,web-task-gate}.ts：网页采集的编排。
//
// 本切片覆盖「静态路由」：规范化入口 → 逐跳抓取（SSRF 钉扎、目录范围、10 MiB 上限）→ 严格解码 → 静态判定 →
// 提取。需要浏览器渲染的页面（静态判定为 render、解码失败、静态提取为空）经 webRenderer 接口交给渲染进程，
// 渲染切片接入前该接口为空，按"网页需要浏览器渲染"如实失败，不假装成功。
//
// 与 TS 版的差别：
//   - 请求头带 gzip/deflate 而不带 br（Go 标准库无 brotli）；
//   - 正文提取在进程内协程完成（TS 为独立 worker 线程），45 s 超时、16 MiB 输入上限、panic 捕获；
//   - 严格解码对非 UTF-8 编码以"出现 U+FFFD"近似 TextDecoder 的 fatal（见 decodeWebStrict）。

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	whatwg "github.com/nlnwa/whatwg-url/url"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/encoding/simplifiedchinese"

	"guizhi-mygo-shell/extract"
)

const (
	webResponseLimit   = 10 << 20
	webExtractFrame    = 16 << 20
	webExtractTimeout  = 45 * time.Second
	webCaptureTimeout  = 120 * time.Second
	webVerifyTimeout   = 300 * time.Second
	webCaptureParallel = 2
	webMaxHops         = 5
	// webDocumentAccept 对应 DOCUMENT_ACCEPT：Chromium 对文档导航发出的 Accept。
	webDocumentAccept = "text/html,application/xhtml+xml,application/xml;q=0.9,image/jxl,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7"
	// webVerificationRequired 对应 WEB_VERIFICATION_REQUIRED。
	webVerificationRequired = "web_verification_required"
)

// webUserAgent 与渲染窗口同一身份（自报家门，不冒用爬虫身份）。
func webUserAgent() string {
	return "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) GuiZhi/" + appVersion + " Chrome/156.0.0.0 Safari/537.36"
}

type webCaptureRequest struct {
	TaskID                  string
	Purpose                 string
	URL                     string
	Scope                   *webScope
	InteractiveVerification bool
}

type renderedWebPage struct {
	HTML   string
	URL    string
	Status int
	Links  []string
}

// webRenderer 由渲染切片实现：用独立的离屏渲染进程取得执行脚本后的页面。
type webRenderer interface {
	Render(ctx context.Context, req webCaptureRequest) (*renderedWebPage, error)
}

// ---- 并发与超时（对应 captureWebPage 里的 active/origins 与 WebTaskGate） ----

type webCaptureGate struct {
	mu      sync.Mutex
	active  int
	origins map[string]bool
	wake    chan struct{}
}

func newWebCaptureGate() *webCaptureGate {
	return &webCaptureGate{origins: map[string]bool{}, wake: make(chan struct{})}
}

// enter 最多 2 个并发、同源串行；ctx 结束即放弃等待。
func (g *webCaptureGate) enter(ctx context.Context, origin string) error {
	for {
		g.mu.Lock()
		if g.active < webCaptureParallel && !g.origins[origin] {
			g.active++
			g.origins[origin] = true
			g.mu.Unlock()
			return nil
		}
		wake := g.wake
		g.mu.Unlock()
		select {
		case <-wake:
		case <-ctx.Done():
			return errors.New("网页采集等待已取消或超时")
		}
	}
}

func (g *webCaptureGate) leave(origin string) {
	g.mu.Lock()
	g.active--
	delete(g.origins, origin)
	close(g.wake)
	g.wake = make(chan struct{})
	g.mu.Unlock()
}

var webGate = newWebCaptureGate()

// webAbortFailure 对应 webAbortError：取消与超时带显式归类。
func webAbortFailure(ctx context.Context) error {
	if errors.Is(context.Cause(ctx), context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return newWebCaptureFailure("网页采集超时", "timeout")
	}
	return newWebCaptureFailure("网页采集已取消", "canceled")
}

// ---- 提取（对应 WebNodeExtractor：同时只跑一个，45 s 超时） ----

var webExtractGate = make(chan struct{}, 1)

type extractFailure struct{ msg string }

func (e *extractFailure) Error() string { return e.msg }

// extractWebBody 在进程内协程里提取，超时/取消后协程自行结束（CPU 密集且无取消点，结果被丢弃）。
func extractWebBody(ctx context.Context, htmlText, pageURL string, status int) (webCaptureResult, error) {
	if len(htmlText) > webExtractFrame {
		return webCaptureResult{}, &extractFailure{"正文提取输入超过 16 MiB"}
	}
	select {
	case webExtractGate <- struct{}{}:
	case <-ctx.Done():
		return webCaptureResult{}, webAbortFailure(ctx)
	}
	type outcome struct {
		r   webCaptureResult
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		defer func() { <-webExtractGate }()
		defer func() {
			if rec := recover(); rec != nil {
				name := "未知异常"
				if e, ok := rec.(error); ok {
					name = panicTypeName(e)
				}
				done <- outcome{err: &extractFailure{fmt.Sprintf("正文提取失败（%s）", name)}}
			}
		}()
		done <- outcome{r: extractWebPage(htmlText, pageURL, status)}
	}()
	timer := time.NewTimer(webExtractTimeout)
	defer timer.Stop()
	select {
	case o := <-done:
		if o.err == nil && len(o.r.Markdown)*2 > webExtractFrame {
			return webCaptureResult{}, &extractFailure{"正文提取输出超过 16 MiB"}
		}
		return o.r, o.err
	case <-timer.C:
		return webCaptureResult{}, errors.New("正文提取超时")
	case <-ctx.Done():
		return webCaptureResult{}, webAbortFailure(ctx)
	}
}

var rePanicIdent = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)

// panicTypeName 只带出异常类名，避免把页面内容带进错误信息。
func panicTypeName(e error) string {
	n := fmt.Sprintf("%T", e)
	n = strings.TrimPrefix(n, "*")
	if i := strings.LastIndex(n, "."); i >= 0 {
		n = n[i+1:]
	}
	if rePanicIdent.MatchString(n) {
		return n
	}
	return "未知异常"
}

// ---- 字符集（对应 TextDecoder(charset, {fatal:true})） ----

var reWebCharset = regexp.MustCompile(`(?i)charset=["']?([^;\s"']+)`)

// whatwgDecoderFor：WHATWG 里 gbk/gb2312 标签的解码器是 GB18030（GBK 的超集）。
func whatwgDecoderFor(enc encoding.Encoding) encoding.Encoding {
	if enc == simplifiedchinese.GBK {
		return simplifiedchinese.GB18030
	}
	return enc
}

// decodeWebStrict 严格解码；无法解码返回 false（调用方转渲染路由）。
// UTF-8 精确（含 BOM 剥离）；其它编码以输出出现 U+FFFD 近似 fatal。
func decodeWebStrict(body []byte, contentType string) (string, bool) {
	label := "utf-8"
	if m := reWebCharset.FindStringSubmatch(contentType); m != nil {
		label = m[1]
	}
	enc, err := htmlindex.Get(strings.ToLower(strings.TrimSpace(label)))
	if err != nil {
		return "", false
	}
	name, _ := htmlindex.Name(enc)
	switch name {
	case "replacement":
		return "", false
	case "utf-8":
		if !utf8.Valid(body) {
			return "", false
		}
		return string(bytes.TrimPrefix(body, []byte{0xEF, 0xBB, 0xBF})), true
	}
	out, err := whatwgDecoderFor(enc).NewDecoder().Bytes(body)
	if err != nil || bytes.ContainsRune(out, utf8.RuneError) {
		return "", false
	}
	return string(out), true
}

// ---- 网络（对应 webNetworkRequest） ----

type webHop struct {
	Status  int
	Headers map[string]string // 小写键
	Body    []byte
}

func webIdentityOptions() safeRequestOptions {
	o := safeRequestOptions{AcceptEncoding: "gzip, deflate", NoLanguage: true, Accept: webDocumentAccept}
	if os.Getenv("GUIZHI_WEB_IDENTITY") == "none" {
		o.UserAgent = "Go-http-client/1.1"
		o.Accept = "*/*"
		return o
	}
	o.UserAgent = webUserAgent()
	return o
}

// webRequest 单跳 GET：公网校验+钉扎、30 s 空闲超时、响应与解压后均不超过 10 MiB；3xx 原样返回由调用方处理。
func (f *safeFetcher) webRequest(ctx context.Context, rawURL string, o safeRequestOptions) (webHop, error) {
	parsed, err := whatwg.Parse(rawURL)
	if err != nil {
		return webHop{}, errors.New("Invalid URL")
	}
	nu, err := toNetURL(parsed)
	if err != nil {
		return webHop{}, errors.New("Invalid URL")
	}
	viaProxy := f.proxy.ProxyFor(nu) != nil
	pinned, err := f.assertSafeTarget(ctx, parsed, viaProxy)
	if err != nil {
		return webHop{}, err
	}
	resp, err := f.openRequest(ctx, parsed, pinned, viaProxy, o)
	if err != nil {
		return webHop{}, err
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
		lk := strings.ToLower(k)
		switch lk {
		case "content-encoding", "content-length", "transfer-encoding", "connection", "alt-svc":
			continue
		}
		h[lk] = strings.Join(vs, "\n")
	}
	return webHop{Status: resp.StatusCode, Headers: h, Body: data}, nil
}

// decodeWebContentEncoding 解压响应体：gzip / deflate（Go 无 brotli，请求头不声明 br），解压后同样受 10 MiB 上限约束。
func decodeWebContentEncoding(raw []byte, encoding string) ([]byte, error) {
	var err error
	switch enc := strings.ToLower(encoding); enc {
	case "", "identity":
		return raw, nil
	case "gzip", "deflate":
		var r io.Reader
		if enc == "gzip" {
			r, err = gzip.NewReader(bytes.NewReader(raw))
		} else if r, err = zlib.NewReader(bytes.NewReader(raw)); err != nil {
			// 个别服务器发送裸 deflate，Node 的 inflateSync 会失败；这里多容一层，结果只会更宽松
			r, err = flate.NewReader(bytes.NewReader(raw)), nil
		}
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(io.LimitReader(r, webResponseLimit+1))
		if err != nil {
			return nil, err
		}
		if len(data) > webResponseLimit {
			return nil, errors.New("解压后的网页超过 10 MiB")
		}
		return data, nil
	default:
		return nil, errors.New("不支持的响应压缩格式")
	}
}

var reWebBinaryType = regexp.MustCompile(`(?i)application/(?:pdf|json|octet-stream)`)

// staticPage 对应 ElectronWebCapture.staticPage：返回 nil 表示需要渲染。
func (f *safeFetcher) staticPage(ctx context.Context, req webCaptureRequest) (*renderedWebPage, error) {
	url := req.URL
	opts := webIdentityOptions()
	for hop := 0; hop <= webMaxHops; hop++ {
		if req.Scope != nil && !inWebScope(url, *req.Scope) {
			return nil, errors.New("重定向超出目录范围")
		}
		resp, err := f.webRequest(ctx, url, opts)
		if err != nil {
			return nil, err
		}
		if loc := resp.Headers["location"]; loc != "" && (resp.Status == 301 || resp.Status == 302 || resp.Status == 303 || resp.Status == 307 || resp.Status == 308) {
			next, err := whatwg.ParseRef(url, loc)
			if err != nil {
				return nil, errors.New("Invalid URL")
			}
			if url, err = canonicalWebURL(next.Href(false)); err != nil {
				return nil, err
			}
			continue
		}
		contentType := resp.Headers["content-type"]
		if reWebBinaryType.MatchString(contentType) {
			return nil, newWebCaptureFailure("入口返回非网页内容", "incomplete")
		}
		text, ok := decodeWebStrict(resp.Body, contentType)
		if !ok {
			return nil, nil
		}
		decision := extract.AssessStaticPage(text, resp.Status)
		if decision.Route == "render" {
			return nil, nil
		}
		return &renderedWebPage{HTML: text, URL: url, Status: resp.Status, Links: extract.StaticPageLinks(text, url)}, nil
	}
	return nil, errors.New("网页重定向超过 5 次")
}

// ---- 编排 ----

func webVerificationFailure() error {
	return newWebCaptureFailure(webVerificationRequired+"：网站需要网页验证，点击「验证并自动采集」后完成验证。", "captcha")
}

// errWebRenderUnavailable：渲染进程尚未接入时，需要渲染的页面如实失败。
var errWebRenderUnavailable = newWebCaptureFailure("网页需要浏览器渲染才能获取正文，当前版本尚未接入渲染进程", "empty")

func (l *Library) renderWebPage(ctx context.Context, req webCaptureRequest) (*renderedWebPage, error) {
	if l.webRenderer == nil {
		return nil, errWebRenderUnavailable
	}
	return l.webRenderer.Render(ctx, req)
}

// captureWebPage 对应 web-capture.ts captureWebPage + ElectronWebCapture.capture：失败不抛错，折成带 error 的结果。
func (l *Library) captureWebPage(parent context.Context, req webCaptureRequest, stage func(string)) webCaptureResult {
	if stage == nil {
		stage = func(string) {}
	}
	if isWechatURL(req.URL) {
		result, err := l.captureWechat(parent, req.URL, req.TaskID)
		if err != nil {
			return l.failWebCapture(req, err)
		}
		return result
	}
	entry, err := canonicalWebURL(req.URL)
	if err != nil {
		// 与 TS 一致：canonicalWebUrl 在 captureWebPage 里直接抛出（不折成结果）；这里折成结果便于调用方统一处理
		return failedWebResult(req, req.URL, webCaptureErrorOf(err))
	}
	req.URL = entry
	origin := webOrigin(entry)
	limit := webCaptureTimeout
	if req.InteractiveVerification {
		limit = webVerifyTimeout
	}
	ctx, cancel := context.WithTimeout(parent, limit)
	defer cancel()
	if err := webGate.enter(ctx, origin); err != nil {
		return l.failWebCapture(req, err)
	}
	defer webGate.leave(origin)
	result, err := l.captureWebLocked(ctx, req, stage)
	if err != nil {
		if ctx.Err() != nil {
			err = webAbortFailure(ctx)
		}
		return l.failWebCapture(req, err)
	}
	return result
}

func webOrigin(u string) string {
	p, err := whatwg.Parse(u)
	if err != nil {
		return u
	}
	return p.Protocol() + "//" + p.Host()
}

func failedWebResult(req webCaptureRequest, url string, failure webCaptureError) webCaptureResult {
	return webCaptureResult{
		TaskID: req.TaskID, EntryURL: url, FinalURL: url, Title: url, DateConfidence: "unknown",
		Links: []string{}, Paragraphs: []webParagraph{}, CapturedAt: nowMs(), EngineVersion: webExtractEngine,
		Warnings: []string{}, Error: &failure,
	}
}

func (l *Library) failWebCapture(req webCaptureRequest, err error) webCaptureResult {
	failure := webCaptureErrorOf(err)
	if req.InteractiveVerification && failure.Code != "security" && !strings.Contains(failure.Message, webVerificationRequired) {
		failure.Message = webVerificationRequired + "：" + failure.Message + "。可再次点击「验证并自动采集」。"
	}
	logAppErrorf("main", "网页采集", "%s", failure.Message)
	return failedWebResult(req, req.URL, failure)
}

func (l *Library) captureWebLocked(ctx context.Context, req webCaptureRequest, stage func(string)) (webCaptureResult, error) {
	if req.InteractiveVerification {
		// 验证浏览器与 FlareSolverr 后备服务属渲染/验证切片
		return webCaptureResult{}, newWebCaptureFailure("交互式网页验证尚未接入当前版本", "empty")
	}
	stage("fetching")
	fetcher := l.webFetcher
	if fetcher == nil {
		fetcher = defaultSafeFetcher
	}
	page, err := fetcher.staticPage(ctx, req)
	if err != nil {
		return webCaptureResult{}, err
	}
	engine := "static"
	if page == nil {
		if page, err = l.renderWebPage(ctx, req); err != nil {
			return webCaptureResult{}, err
		}
		engine = "electron"
	}
	if extract.IsWebVerificationPage(page.HTML) {
		return webCaptureResult{}, webVerificationFailure()
	}
	stage("extracting")
	result, err := extractWebBody(ctx, page.HTML, page.URL, page.Status)
	if err != nil {
		return webCaptureResult{}, err
	}
	if engine == "static" && result.Error != nil && result.Error.Code == "empty" {
		stage("fetching")
		rendered, rerr := l.renderWebPage(ctx, req)
		if rerr != nil {
			if errors.Is(rerr, errWebRenderUnavailable) {
				// 没有渲染进程可回退：保留静态提取的"正文为空"结果，由导入侧给出降级原因
				rendered = nil
			} else {
				return webCaptureResult{}, rerr
			}
		}
		if rendered != nil {
			page, engine = rendered, "electron"
			if extract.IsWebVerificationPage(page.HTML) {
				return webCaptureResult{}, webVerificationFailure()
			}
			stage("extracting")
			if result, err = extractWebBody(ctx, page.HTML, page.URL, page.Status); err != nil {
				return webCaptureResult{}, err
			}
		}
	}
	if ctx.Err() != nil {
		return webCaptureResult{}, webAbortFailure(ctx)
	}
	result.TaskID, result.EntryURL, result.FinalURL = req.TaskID, req.URL, page.URL
	result.Links = page.Links
	if result.Links == nil {
		result.Links = []string{}
	}
	result.CapturedAt = nowMs()
	result.EngineVersion = webExtractEngine + "-" + engine
	return result, nil
}

// webCaptureToExtracted 对应 import-service 的 captureWebpage 回调：把采集结果交给导入队列。
func webCaptureToExtracted(r webCaptureResult) *extractedContent {
	ex := &extractedContent{Title: r.Title, Content: r.Markdown, ItemType: "webpage", WebCapture: &r}
	final := r.FinalURL
	ex.SourceURI = &final
	if r.Error != nil {
		ex.DegradedReason = r.Error.Message
	}
	ex.WarningReason = strings.Join(r.Warnings, "；")
	return ex
}
