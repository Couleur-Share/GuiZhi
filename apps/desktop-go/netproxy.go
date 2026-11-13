package main

// 对应 shared/utils/network-proxy.ts（设置归一化、绕过规则、代理 URL）与
// main/services/network-proxy.ts 里主进程出站 HTTP 的部分（applyNetworkProxySettings、
// getHttpRequestAgent、hasAnyProxyConfigured、环境变量同步）。
//
// 模式语义与 Electron 版一致：
//   - system：Node 主进程请求并不读系统代理，所以直连；仅 hasAnyProxyConfigured 为真（fake-ip 放行依据）。
//   - direct：直连。
//   - manual：HTTP/HTTPS/SOCKS5（Go 的 socks5 由代理侧解析域名，对应 TS 的 socks5h）；命中绕过规则的主机直连。
// 不含：Electron 会话代理（渲染进程不出网）、采集进程的 --proxy-server（随采集进程切片接入）。

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"

	whatwg "github.com/nlnwa/whatwg-url/url"
)

type networkProxySettings struct {
	Mode     string `json:"mode"`
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	Bypass   string `json:"bypass"`
}

var defaultNetworkProxy = networkProxySettings{
	Mode: "system", Protocol: "http", Port: 7890, Bypass: "<local>,localhost,127.0.0.1,::1",
}

var (
	proxyCtrlRe   = regexp.MustCompile("[\r\n\x00-\x1f\x7f]")
	bypassCtrlRe  = regexp.MustCompile("[\x00-\x09\x0b-\x1f\x7f]")
	bypassSplitRe = regexp.MustCompile(`[,;\r\n]+`)
)

func sanitizeProxyText(v any, max int) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return utf16Slice(jsTrim(proxyCtrlRe.ReplaceAllString(s, "")), max)
}

// jsParseInt10 对应 Number.parseInt(s, 10)：前导空白、可选符号、数字前缀；无数字为 NaN。
func jsParseInt10(s string) (float64, bool) {
	s = strings.TrimLeftFunc(s, jsSpace)
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	start := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == start {
		return 0, false
	}
	f, err := strconv.ParseFloat(s[:i], 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

func normalizeProxyPort(v any) int {
	var f float64
	switch t := v.(type) {
	case float64:
		f = t
	case string:
		n, ok := jsParseInt10(t)
		if !ok {
			return defaultNetworkProxy.Port
		}
		f = n
	default:
		return defaultNetworkProxy.Port
	}
	if f != math.Trunc(f) || f < 1 || f > 65535 {
		return defaultNetworkProxy.Port
	}
	return int(f)
}

func normalizeProxyBypass(v any) string {
	s, _ := v.(string)
	s = utf16Slice(jsTrim(bypassCtrlRe.ReplaceAllString(s, "")), 2048)
	var out []string
	for _, e := range bypassSplitRe.Split(s, -1) {
		if e = jsTrim(e); e != "" {
			out = append(out, e)
		}
	}
	return strings.Join(out, ",")
}

type parsedProxyHost struct {
	protocol, host, username, password *string
	port                               any // 未设置时为 nil
}

// parseProxyHostURL 对应 parseProxyHostUrl：host 字段里直接写 scheme://user:pass@host:port。
func parseProxyHostURL(host string) parsedProxyHost {
	if !strings.Contains(host, "://") {
		return parsedProxyHost{host: &host}
	}
	u, err := whatwg.Parse(host)
	if err != nil {
		return parsedProxyHost{host: &host}
	}
	protocol := strings.TrimSuffix(u.Protocol(), ":")
	if protocol != "http" && protocol != "https" && protocol != "socks5" {
		protocol = defaultNetworkProxy.Protocol
	}
	user, err1 := url.PathUnescape(u.Username())
	pass, err2 := url.PathUnescape(u.Password())
	if err1 != nil || err2 != nil { // decodeURIComponent 抛错 → 整体按原样当主机名
		return parsedProxyHost{host: &host}
	}
	hostname := u.Hostname()
	out := parsedProxyHost{protocol: &protocol, host: &hostname, username: &user, password: &pass}
	if p := u.Port(); p != "" {
		out.port = p
	}
	return out
}

// normalizeNetworkProxy 对应 normalizeNetworkProxySettings；入参为 JSON 解码后的任意值。
func normalizeNetworkProxy(value any) networkProxySettings {
	rec, _ := value.(map[string]any)
	if rec == nil {
		rec = map[string]any{}
	}
	parsed := parseProxyHostURL(sanitizeProxyText(rec["host"], 512))

	out := networkProxySettings{}
	if m, _ := rec["mode"].(string); m == "system" || m == "direct" || m == "manual" {
		out.Mode = m
	} else {
		out.Mode = defaultNetworkProxy.Mode
	}
	proto := rec["protocol"]
	if parsed.protocol != nil {
		proto = *parsed.protocol
	}
	if p, _ := proto.(string); p == "http" || p == "https" || p == "socks5" {
		out.Protocol = p
	} else {
		out.Protocol = defaultNetworkProxy.Protocol
	}
	var hostV any = rec["host"]
	if parsed.host != nil {
		hostV = *parsed.host
	}
	out.Host = sanitizeProxyText(hostV, 512)
	port := rec["port"]
	if parsed.port != nil {
		port = parsed.port
	}
	out.Port = normalizeProxyPort(port)
	var userV, passV any = rec["username"], rec["password"]
	if parsed.username != nil {
		userV = *parsed.username
	}
	if parsed.password != nil {
		passV = *parsed.password
	}
	out.Username = sanitizeProxyText(userV, 512)
	out.Password = sanitizeProxyText(passV, 1024)
	if out.Bypass = normalizeProxyBypass(rec["bypass"]); out.Bypass == "" {
		out.Bypass = defaultNetworkProxy.Bypass
	}
	return out
}

func encodeURIComponent(s string) string {
	// url.QueryEscape 把空格写成 +，且会转义 JS 保留的 !'()*~；逐字符按 encodeURIComponent 的保留集处理
	const keep = "-_.!~*'()"
	var b strings.Builder
	for _, c := range []byte(s) {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte(keep, c) >= 0 {
			b.WriteByte(c)
		} else {
			b.WriteString("%" + strings.ToUpper(strconv.FormatInt(int64(c)+256, 16)[1:]))
		}
	}
	return b.String()
}

// buildProxyURL 对应 buildProxyUrl：非 manual 或缺主机/端口返回 ""。
func buildProxyURL(p networkProxySettings) string {
	p = normalizeNetworkProxy(map[string]any{"mode": p.Mode, "protocol": p.Protocol, "host": p.Host,
		"port": float64(p.Port), "username": p.Username, "password": p.Password, "bypass": p.Bypass})
	if p.Mode != "manual" || p.Host == "" || p.Port == 0 {
		return ""
	}
	host := p.Host
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	auth := ""
	if p.Username != "" || p.Password != "" {
		auth = encodeURIComponent(p.Username) + ":" + encodeURIComponent(p.Password) + "@"
	}
	return p.Protocol + "://" + auth + host + ":" + strconv.Itoa(p.Port)
}

// shouldBypassProxy 对应 shouldBypassProxyForHostname。
func shouldBypassProxy(hostname, bypass string) bool {
	h := strings.ToLower(jsTrim(hostname))
	if h == "" {
		return false
	}
	for _, rule := range bypassSplitRe.Split(normalizeProxyBypass(bypass), -1) {
		rule = strings.ToLower(jsTrim(rule))
		switch {
		case rule == "":
		case rule == "<local>":
			if !strings.Contains(h, ".") {
				return true
			}
		case rule == h:
			return true
		case strings.HasPrefix(rule, "*."):
			if strings.HasSuffix(h, rule[1:]) {
				return true
			}
		case strings.HasPrefix(rule, "."):
			if strings.HasSuffix(h, rule) {
				return true
			}
		}
	}
	return false
}

// ---- 运行期状态 ----

var proxyEnvKeys = []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "all_proxy", "no_proxy"}

type netProxyState struct {
	mu          sync.RWMutex
	active      networkProxySettings
	proxyURL    *url.URL
	origEnv     map[string]*string
	envCaptured bool
}

// activeNetProxy 对应 network-proxy.ts 的模块级 activeNetworkProxy。
var activeNetProxy = newNetProxyState()

func newNetProxyState() *netProxyState {
	return &netProxyState{active: normalizeNetworkProxy(nil)}
}

// Apply 对应 applyNetworkProxySettings（不含 Electron 会话部分）。
func (s *netProxyState) Apply(value any) networkProxySettings {
	settings := normalizeNetworkProxy(value)
	var parsed *url.URL
	if rawURL := buildProxyURL(settings); rawURL != "" {
		parsed, _ = url.Parse(rawURL)
	}
	s.mu.Lock()
	s.active = settings
	s.proxyURL = parsed
	s.applyEnvLocked(settings, urlStr(parsed))
	s.mu.Unlock()
	return settings
}

func urlStr(u *url.URL) string {
	if u == nil {
		return ""
	}
	return u.String()
}

// applyEnvLocked 对应 applyProxyEnvironment：给 yt-dlp、ffmpeg、pip 等子进程继承；首次调用前保存原值，非手动时还原。
func (s *netProxyState) applyEnvLocked(settings networkProxySettings, proxyURL string) {
	if !s.envCaptured {
		s.envCaptured = true
		s.origEnv = map[string]*string{}
		for _, k := range proxyEnvKeys {
			if v, ok := os.LookupEnv(k); ok {
				vv := v
				s.origEnv[k] = &vv
			} else {
				s.origEnv[k] = nil
			}
		}
	}
	set := func(k string, v *string) {
		if v == nil {
			_ = os.Unsetenv(k)
		} else {
			_ = os.Setenv(k, *v)
		}
	}
	if settings.Mode != "manual" || proxyURL == "" {
		for _, k := range proxyEnvKeys {
			set(k, s.origEnv[k])
		}
		return
	}
	for _, k := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		set(k, &proxyURL)
	}
	bypass := settings.Bypass
	set("NO_PROXY", &bypass)
	set("no_proxy", &bypass)
}

func (s *netProxyState) Settings() networkProxySettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active
}

// HasAnyProxyConfigured 对应 hasAnyProxyConfigured：非 direct 即视为存在代理配置。
func (s *netProxyState) HasAnyProxyConfigured() bool { return s.Settings().Mode != "direct" }

// ProxyFor 对应 getManualProxyUrlForTarget：目标需要走手动代理时返回代理 URL，否则 nil。
// hostname 采用 WHATWG 的 URL.hostname 形态（IPv6 带方括号），与绕过表的写法对齐。
func (s *netProxyState) ProxyFor(target *url.URL) *url.URL {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.active.Mode != "manual" || s.proxyURL == nil {
		return nil
	}
	host := strings.ToLower(target.Hostname())
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if shouldBypassProxy(host, s.active.Bypass) {
		return nil
	}
	return s.proxyURL
}

// ProxyFunc 供 http.Transport.Proxy 使用。
func (s *netProxyState) ProxyFunc(req *http.Request) (*url.URL, error) {
	return s.ProxyFor(req.URL), nil
}

// applyStoredNetworkProxy 启动时按库里的 networkProxy 设置生效（对应 index.ts applyStoredNetworkProxySettings）。
func applyStoredNetworkProxy(raw string, ok bool) networkProxySettings {
	var v any
	if ok {
		_ = json.Unmarshal([]byte(raw), &v)
	}
	return activeNetProxy.Apply(v)
}

// newProxiedTransport 返回按 activeNetProxy 选路的传输层（不读环境变量代理：Node 主进程同样不读）。
func newProxiedTransport() *http.Transport {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = activeNetProxy.ProxyFunc
	return tr
}

// readRawSetting 读取 settings 表里某键的原始 JSON 文本。
func (l *Library) readRawSetting(ctx context.Context, key string) (string, bool) {
	rows, err := l.db.Query(ctx, "SELECT value FROM settings WHERE key = ?", key)
	if err != nil || len(rows.Values) == 0 {
		return "", false
	}
	s, ok := rows.Values[0][0].(string)
	return s, ok
}
