package main

// 对应 apps/desktop/src/main/ipc/ai.ipc.ts：AI HTTP/流式请求经主进程代理。
// 频道：ai:httpRequest（invoke）、ai:httpStream（invoke，过程中推送 ai:httpStreamChunk / ai:httpStreamError）、
// ai:httpCancel（send）。语义逐条对照原实现，差分测试见 spikes/mygo-ai。
//
// 与 TS 版刻意不同的地方（都在迁移文档里记录）：
//   - 守卫在拨号时再检查一次实际连上的地址（net.Dialer.Control），所以 DNS 重绑定和重定向到元数据地址都会被挡；
//     TS 版只校验第一跳 URL 的解析结果。
//   - 网络代理设置（settings.networkProxy）经 activeNetProxy 接入：手动代理生效，其余直连。
//   - 错误文案：Node 的 "fetch failed" / "terminated" 在 Go 里按错误发生的阶段还原。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/egoist/mygo"
)

type aiRequest struct {
	RequestID string             `json:"requestId"`
	Method    string             `json:"method"`
	URL       string             `json:"url"`
	Headers   map[string]*string `json:"headers"`
	Body      *string            `json:"body"`
	TimeoutMs *float64           `json:"timeoutMs"`
}

type aiResponse struct {
	OK         bool              `json:"ok"`
	Status     int               `json:"status"`
	StatusText string            `json:"statusText"`
	Body       string            `json:"body"`
	Headers    map[string]string `json:"headers"`
	Error      *string           `json:"error,omitempty"`
}

// aiStreamPayload 是 ai:httpStreamChunk / ai:httpStreamError 的负载（二选一）。
type aiStreamPayload struct {
	RequestID string `json:"requestId"`
	Chunk     string `json:"chunk,omitzero"`
	Error     string `json:"error,omitzero"`
}

// ipcPush 对应垫片里的 "ipc" 事件：{channel, args}，垫片再按频道分发给 ipcRenderer.on 的监听器。
type ipcPush struct {
	Channel string `json:"channel"`
	Args    []any  `json:"args"`
}

var ipcEvent = mygo.NewEvent[ipcPush]("ipc")

const (
	chAIHTTPRequest     = "ai:httpRequest"
	chAIHTTPStream      = "ai:httpStream"
	chAIHTTPStreamChunk = "ai:httpStreamChunk"
	chAIHTTPStreamError = "ai:httpStreamError"
	chAIHTTPCancel      = "ai:httpCancel"

	aiDefaultTimeoutMs = 30_000
)

type forbiddenDialError struct{ addr string }

func (e *forbiddenDialError) Error() string { return "不允许访问该地址: " + e.addr }

// aiCall 是一次在途请求：取消与超时都通过 cause 区分，对应 TS 里 AbortController.abort(reason)。
type aiCall struct {
	ctx    context.Context
	cancel context.CancelCauseFunc
	id     string
}

type aiProxy struct {
	client     *http.Client
	configured func() map[string]bool // 已配置端点的 `scheme://host`，对应 collectConfiguredHosts

	mu            sync.Mutex
	tracked       map[string]*aiCall
	lastUnknownAt int64
	granted       map[string]bool
}

func newAIProxy(configured func() map[string]bool) *aiProxy {
	dialer := &net.Dialer{
		Timeout: 30 * time.Second,
		// 拨号时校验实际要连的地址：守卫的 DNS 解析和真正的 connect 是两次解析，中间可被重绑定；
		// 重定向的下一跳也不会再过 assertSafeAIEndpoint。在这里统一兜底。
		Control: func(network, address string, _ syscall.RawConn) error {
			if ap, err := netip.ParseAddrPort(address); err == nil && isForbiddenAIEndpointAddr(ap.Addr()) {
				return &forbiddenDialError{addr: ap.Addr().String()}
			}
			return nil
		},
	}
	tr := &http.Transport{
		Proxy:               activeNetProxy.ProxyFunc, // 手动代理（含绕过规则）；system/direct 直连
		DialContext:         dialer.DialContext,
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: 10 * time.Second,
		MaxIdleConnsPerHost: 4,
		IdleConnTimeout:     90 * time.Second,
	}
	return &aiProxy{
		client:     &http.Client{Transport: tr},
		configured: configured,
		tracked:    map[string]*aiCall{},
		granted:    map[string]bool{},
	}
}

func (p *aiProxy) register(b *Bridge) {
	b.handlers[chAIHTTPRequest] = func(ctx context.Context, args []json.RawMessage) (any, error) {
		return p.request(ctx, decodeAIRequest(args)), nil
	}
	b.handlers[chAIHTTPStream] = func(ctx context.Context, args []json.RawMessage) (any, error) {
		return p.stream(ctx, decodeAIRequest(args)), nil
	}
	b.sends[chAIHTTPCancel] = func(args []json.RawMessage) {
		var id string
		if len(args) > 0 && json.Unmarshal(args[0], &id) == nil {
			p.cancel(id)
		}
	}
}

func decodeAIRequest(args []json.RawMessage) aiRequest {
	var r aiRequest
	if len(args) > 0 {
		_ = json.Unmarshal(args[0], &r)
	}
	return r
}

// ---- 追踪与取消（request-cancellation.ts） ----

func (p *aiProxy) begin(parent context.Context, id string) (*aiCall, func()) {
	ctx, cancel := context.WithCancelCause(parent)
	call := &aiCall{ctx: ctx, cancel: cancel, id: id}
	if id != "" {
		p.mu.Lock()
		p.tracked[id] = call
		p.mu.Unlock()
	}
	return call, func() {
		if id != "" {
			p.mu.Lock()
			if p.tracked[id] == call {
				delete(p.tracked, id)
			}
			p.mu.Unlock()
		}
		cancel(nil)
	}
}

func (p *aiProxy) cancel(id string) {
	p.mu.Lock()
	call := p.tracked[id]
	p.mu.Unlock()
	if call != nil {
		call.cancel(errors.New("已取消"))
	}
}

// ---- 守卫（assertSafeAIEndpoint + assertEndpointAllowed） ----

func (p *aiProxy) assertSafe(ctx context.Context, rawURL string) error {
	u, err := parseEndpointURL(rawURL)
	if err != nil {
		return fmt.Errorf("无效的接口地址: %s", rawURL)
	}
	if u.Protocol != "http:" && u.Protocol != "https:" {
		return fmt.Errorf("不支持的协议: %s", u.Protocol)
	}
	var addrs []netip.Addr
	if u.IsIP {
		addrs = []netip.Addr{u.IP}
	} else {
		hostname := strings.TrimSuffix(u.Hostname, ".") // 带点的绝对域名交给解析器
		addrs, err = net.DefaultResolver.LookupNetIP(ctx, "ip", hostname)
		if err != nil {
			return err
		}
	}
	for _, a := range addrs {
		if isForbiddenAIEndpointAddr(a) {
			return fmt.Errorf("不允许访问该地址: %s", u.Host)
		}
	}
	if err := p.assertEndpointAllowed(rawURL); err != nil {
		return err
	}
	for _, a := range addrs {
		if a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() {
			log.Printf("[ai] 请求私网端点: %s//%s", u.Protocol, u.Host)
			break
		}
	}
	return nil
}

func (p *aiProxy) assertEndpointAllowed(rawURL string) error {
	var configured map[string]bool
	if p.configured != nil {
		configured = p.configured()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now().UnixMilli()
	d := evaluateEndpointAccess(rawURL, now, configured, p.granted, p.lastUnknownAt)
	if !d.Allowed {
		reason := d.Reason
		if reason == "" {
			reason = "不允许访问该地址"
		}
		return errors.New(reason)
	}
	if !d.IsKnown && d.Host != "" {
		p.lastUnknownAt = now
		p.granted[d.Host] = true
		log.Printf("[ai] 放行未配置端点（限速）: %s", d.Host)
	}
	return nil
}

// ---- 请求 ----

// started 是已拿到响应头的请求；finish 在响应体读完后撤掉超时计时器。
type started struct {
	resp   *http.Response
	finish func()
}

func (p *aiProxy) perform(call *aiCall, req aiRequest) (*started, error) {
	if err := p.assertSafe(call.ctx, req.URL); err != nil {
		if cause := context.Cause(call.ctx); cause != nil {
			err = cause
		}
		return nil, err // 守卫的可读错误（含 DNS 失败）原样返回，与 TS 一致
	}
	timeoutMs := float64(aiDefaultTimeoutMs)
	if req.TimeoutMs != nil && *req.TimeoutMs > 0 {
		timeoutMs = *req.TimeoutMs
	}
	// 与 TS 一致：计时从守卫通过后开始，覆盖响应头与响应体，直到 finish
	timer := time.AfterFunc(time.Duration(timeoutMs*float64(time.Millisecond)), func() {
		call.cancel(fmt.Errorf("AI 请求超时（%sms）", strconv.FormatFloat(timeoutMs, 'f', -1, 64)))
	})

	method := req.Method
	if method == "" {
		method = "GET"
	}
	var body io.Reader
	if req.Body != nil {
		body = strings.NewReader(*req.Body)
	}
	httpReq, err := http.NewRequestWithContext(call.ctx, method, req.URL, body)
	if err != nil {
		timer.Stop()
		return nil, err
	}
	hasContentType, hasUA := false, false
	for k, v := range req.Headers {
		if v == nil {
			continue // 与 normalizeHeaders 一致：null 值的头不发送
		}
		httpReq.Header.Set(k, *v)
		switch strings.ToLower(k) {
		case "content-type":
			hasContentType = true
		case "user-agent":
			hasUA = true
		}
	}
	if !hasUA {
		httpReq.Header.Set("User-Agent", "node") // undici 的默认值
	}
	if req.Body != nil && *req.Body != "" && !hasContentType {
		httpReq.Header.Set("Content-Type", "text/plain;charset=UTF-8") // undici 对字符串 body 的默认值
	}
	resp, err := p.client.Do(httpReq)
	if err != nil {
		timer.Stop()
		return nil, p.classify(call, err, "fetch failed")
	}
	return &started{resp: resp, finish: func() { timer.Stop(); resp.Body.Close() }}, nil
}

// classify 还原 Node 里 fetch 失败时的 error.message。
func (p *aiProxy) classify(call *aiCall, err error, fallback string) error {
	if cause := context.Cause(call.ctx); cause != nil {
		return cause // 已取消 / 超时 / 父级关闭，原因即文案
	}
	var fd *forbiddenDialError
	if errors.As(err, &fd) {
		return fd
	}
	return errors.New(fallback)
}

func headersToMap(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k, v := range h {
		out[strings.ToLower(k)] = strings.Join(v, ", ")
	}
	return out
}

func statusText(resp *http.Response) string {
	return strings.TrimPrefix(resp.Status, strconv.Itoa(resp.StatusCode)+" ")
}

func errorResponse(err error) aiResponse {
	msg := err.Error()
	return aiResponse{Headers: map[string]string{}, Error: &msg}
}

// readWhole 对应 requestToResponse：读完整个响应体并按 UTF-8 解码。
func (p *aiProxy) readWhole(call *aiCall, resp *http.Response) (aiResponse, error) {
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return aiResponse{}, p.classify(call, err, "terminated")
	}
	var dec utf8StreamDecoder
	return aiResponse{
		OK:         resp.StatusCode >= 200 && resp.StatusCode < 300,
		Status:     resp.StatusCode,
		StatusText: statusText(resp),
		Body:       dec.write(data, true),
		Headers:    headersToMap(resp.Header),
	}, nil
}

func (p *aiProxy) request(ctx context.Context, req aiRequest) aiResponse {
	call, release := p.begin(ctx, req.RequestID)
	defer release()
	s, err := p.perform(call, req)
	if err != nil {
		return errorResponse(err)
	}
	defer s.finish()
	out, err := p.readWhole(call, s.resp)
	if err != nil {
		return errorResponse(err)
	}
	return out
}

func (p *aiProxy) stream(ctx context.Context, req aiRequest) aiResponse {
	call, release := p.begin(ctx, req.RequestID)
	defer release()
	page := mygo.CallerPage(ctx)
	push := func(channel string, payload aiStreamPayload) {
		if page == nil {
			return
		}
		payload.RequestID = req.RequestID
		if err := ipcEvent.EmitPage(page, ipcPush{Channel: channel, Args: []any{payload}}); err != nil {
			log.Printf("[ai] 推送 %s 失败：%v", channel, err)
		}
	}
	fail := func(err error) aiResponse {
		push(chAIHTTPStreamError, aiStreamPayload{Error: err.Error()})
		return errorResponse(err)
	}

	s, err := p.perform(call, req)
	if err != nil {
		return fail(err)
	}
	defer s.finish()
	resp := s.resp
	ok := resp.StatusCode >= 200 && resp.StatusCode < 300
	noBody := resp.StatusCode == 204 || resp.StatusCode == 205 || resp.StatusCode == 304 || req.Method == "HEAD"
	if !ok || noBody {
		// 非 2xx 或没有响应体：整体返回，不走流（与 TS 一致，此分支读取失败才会产生 error 事件）
		out, err := p.readWhole(call, resp)
		if err != nil {
			return fail(err)
		}
		return out
	}

	var dec utf8StreamDecoder
	buf := make([]byte, 32*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if chunk := dec.write(buf[:n], false); chunk != "" {
				push(chAIHTTPStreamChunk, aiStreamPayload{Chunk: chunk})
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			// 流读取阶段出错：事件与返回值都是错误（已推送的块保留，与 TS 一致）
			err := p.classify(call, rerr, "terminated")
			push(chAIHTTPStreamError, aiStreamPayload{Error: err.Error()})
			return errorResponse(err)
		}
	}
	if tail := dec.write(nil, true); tail != "" {
		push(chAIHTTPStreamChunk, aiStreamPayload{Chunk: tail})
	}
	return aiResponse{
		OK: true, Status: resp.StatusCode, StatusText: statusText(resp), Headers: headersToMap(resp.Header),
	}
}

// configuredAIHosts 对应 collectConfiguredHosts：ai-models.json 里 provider 与 model 的 apiUrl，规整成 `scheme://host`。
// 读失败当作没有已配置端点（TS 版同样只告警）。
func (l *Library) configuredAIHosts() map[string]bool {
	hosts := map[string]bool{}
	cfg, err := l.readAIConfig()
	if err != nil {
		log.Printf("[ai] 读取已配置端点失败: %v", err)
		return hosts
	}
	for _, list := range [][]*omap{cfg.Providers, cfg.Models} {
		for _, m := range list {
			if raw, ok := m.get("apiUrl"); ok {
				if s, ok := raw.(string); ok {
					if h, ok := normalizeEndpointHost(s); ok {
						hosts[h] = true
					}
				}
			}
		}
	}
	return hosts
}
