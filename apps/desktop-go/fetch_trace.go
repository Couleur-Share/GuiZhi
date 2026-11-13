package main

// 采集出站请求的诊断开关（只由环境变量打开，不是用户设置项，不进设置页）：
//
//	GUIZHI_FETCH_TRACE=<文件>            每一跳请求追加一行 JSON：UA、Referer、状态码、Location、耗时、错误。
//	GUIZHI_FETCH_MIN_INTERVAL_MS=<毫秒>  同一个抓取器的两次请求起点之间至少间隔多久（限速）。
//
// 用途：排查站点侧拦截（例如 NGA 图床 HTTP 567、分页重定向循环）时留下可复核的请求证据，
// 并能在不改代码的情况下放慢节奏。Cookie 只记名字不记值，URL 里疑似机密的查询值打码。
// Diagnostic switches, env-only: per-hop JSONL request trace (cookie values and secret-looking
// query values are masked) and a minimum interval between request starts.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type fetchTraceEntry struct {
	Time        string            `json:"time"`
	Seq         int               `json:"seq"` // 同一次调用（含重定向各跳）共用
	Hop         int               `json:"hop"`
	URL         string            `json:"url"`
	Request     map[string]string `json:"request"`
	KeepAlive   bool              `json:"keepAlive"`
	Status      int               `json:"status"`
	Location    string            `json:"location,omitempty"`
	ContentType string            `json:"contentType,omitempty"`
	// 响应侧只记名字不记值：Set-Cookie 名（重定向跳上下发的会话 Cookie 是自重定向循环的关键线索）、Server、响应头名
	SetCookieNames  []string `json:"setCookieNames,omitempty"`
	Server          string   `json:"server,omitempty"`
	ResponseHeaders []string `json:"responseHeaders,omitempty"`
	ElapsedMS       int64    `json:"elapsedMs"` // 从发出请求到收到响应头
	Error           string   `json:"error,omitempty"`
}

type fetchTracer struct {
	mu  sync.Mutex
	w   io.Writer
	seq int
}

func newFetchTracer(w io.Writer) *fetchTracer { return &fetchTracer{w: w} }

func (t *fetchTracer) nextSeq() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.seq++
	return t.seq
}

func (t *fetchTracer) write(e fetchTraceEntry) {
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	_, _ = t.w.Write(append(b, '\n'))
}

var secretQueryKeyRe = regexp.MustCompile(`(?i)token|key|secret|sig|auth|pass|session|cookie|ticket|code`)

// maskURLQuery 把疑似机密的查询参数值换成 ***，其余原样保留（tid、page 等排查需要）。
func maskURLQuery(raw string) string {
	q := strings.IndexByte(raw, '?')
	if q < 0 {
		return raw
	}
	head, rest := raw[:q+1], raw[q+1:]
	frag := ""
	if h := strings.IndexByte(rest, '#'); h >= 0 {
		rest, frag = rest[:h], rest[h:]
	}
	pairs := strings.Split(rest, "&")
	for i, p := range pairs {
		k, _, ok := strings.Cut(p, "=")
		if ok && secretQueryKeyRe.MatchString(k) {
			pairs[i] = k + "=***"
		}
	}
	return head + strings.Join(pairs, "&") + frag
}

// maskCookieHeader 只留 Cookie 的名字。
func maskCookieHeader(cookie string) string {
	var names []string
	for _, part := range strings.Split(cookie, ";") {
		if k, _, ok := strings.Cut(strings.TrimSpace(part), "="); ok && k != "" {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	return "[names: " + strings.Join(names, ", ") + "]"
}

func (f *safeFetcher) traceHop(seq, hop int, rawURL string, h http.Header, resp *http.Response, err error, elapsed time.Duration) {
	if f.trace == nil {
		return
	}
	e := fetchTraceEntry{
		Time: time.Now().Format(time.RFC3339Nano), Seq: seq, Hop: hop, URL: maskURLQuery(rawURL),
		Request: map[string]string{}, KeepAlive: false, ElapsedMS: elapsed.Milliseconds(),
	}
	for k := range h {
		v := h.Get(k)
		if strings.EqualFold(k, "Cookie") {
			v = maskCookieHeader(v)
		}
		e.Request[k] = v
	}
	if err != nil {
		e.Error = err.Error()
	}
	if resp != nil {
		e.Status = resp.StatusCode
		e.Location = maskURLQuery(resp.Header.Get("Location"))
		e.ContentType = resp.Header.Get("Content-Type")
		e.Server = resp.Header.Get("Server")
		for _, line := range resp.Header.Values("Set-Cookie") {
			if k, _, ok := strings.Cut(strings.TrimSpace(line), "="); ok && k != "" {
				e.SetCookieNames = append(e.SetCookieNames, k)
			}
		}
		for k := range resp.Header {
			e.ResponseHeaders = append(e.ResponseHeaders, k)
		}
		sort.Strings(e.ResponseHeaders)
	}
	f.trace.write(e)
}

// paceWait 保证两次请求起点至少间隔 minInterval；等待期间 ctx 取消则返回「已取消」。
func (f *safeFetcher) paceWait(ctx context.Context) error {
	if f.minInterval <= 0 {
		return nil
	}
	f.paceMu.Lock()
	now := time.Now()
	start := f.paceNext
	if start.Before(now) {
		start = now
	}
	f.paceNext = start.Add(f.minInterval)
	f.paceMu.Unlock()
	wait := time.Until(start)
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return errors.New("已取消")
	case <-timer.C:
		return nil
	}
}

// parseFetchMinInterval 解析毫秒数；空、非数字、非正数都视为不限速。
func parseFetchMinInterval(s string) time.Duration {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 {
		return 0
	}
	return time.Duration(n) * time.Millisecond
}

// applyFetchEnv 读取诊断环境变量；跟踪文件打不开时只在标准错误提示，不影响抓取。
func applyFetchEnv(f *safeFetcher) {
	f.minInterval = parseFetchMinInterval(os.Getenv("GUIZHI_FETCH_MIN_INTERVAL_MS"))
	if p := strings.TrimSpace(os.Getenv("GUIZHI_FETCH_TRACE")); p != "" {
		file, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			_, _ = os.Stderr.WriteString("[fetch] 无法打开请求跟踪文件：" + err.Error() + "\n")
			return
		}
		f.trace = newFetchTracer(file)
	}
}
