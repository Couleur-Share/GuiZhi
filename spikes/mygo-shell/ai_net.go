package main

// 对应 main/ipc/ai.ipc.ts 的 assertSafeAIEndpoint、services/net-safety.ts 的 isForbiddenAIEndpointAddress
// 与 services/ai-endpoint-guard.ts 的 evaluateEndpointAccess。
//
// 难点不在逻辑而在“地址怎么被读出来”：TS 用 WHATWG URL 解析（`http://2852039166/`、`http://0xa9fea9fe/`、
// `http://169.254.169.254./` 都会被规范化成 169.254.169.254），而 Go 的 net/url 只把它们当作域名。
// 守卫是安全边界，两边必须得到同一个地址，所以这里按 WHATWG 的主机解析规则重写了 IPv4 / IPv6 部分，
// 并用 Node 真实输出生成的 golden（ai_net_test.go）逐条核对。

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// endpointURL 是 WHATWG URL 里守卫关心的几个字段。
type endpointURL struct {
	Protocol string // 如 "http:"
	Host     string // 含非默认端口；IPv6 带方括号，与 URL.host 一致
	Hostname string // 与 URL.hostname 一致（IPv6 带方括号）
	IP       netip.Addr
	IsIP     bool
}

var forbiddenHostChars = "\x00\t\n\r #%/:<>?@[\\]^|"

// parseIPv4Number 对应 WHATWG 的 IPv4 number parser。
func parseIPv4Number(s string) (uint64, bool) {
	if s == "" {
		return 0, false
	}
	base := 10
	switch {
	case len(s) >= 2 && (s[:2] == "0x" || s[:2] == "0X"):
		s, base = s[2:], 16
	case len(s) >= 2 && s[0] == '0':
		s, base = s[1:], 8
	}
	if s == "" {
		return 0, true
	}
	n, err := strconv.ParseUint(s, base, 64)
	if err != nil {
		if errors.Is(err, strconv.ErrRange) {
			return 1 << 40, true // 超出 uint64，必然超过 IPv4 上限，由调用方判失败
		}
		return 0, false
	}
	return n, true
}

// endsInANumber 对应 WHATWG 的 “ends in a number checker”。
func endsInANumber(host string) bool {
	parts := strings.Split(host, ".")
	if parts[len(parts)-1] == "" {
		if len(parts) == 1 {
			return false
		}
		parts = parts[:len(parts)-1]
	}
	last := parts[len(parts)-1]
	if last != "" && strings.Trim(last, "0123456789") == "" {
		return true
	}
	_, ok := parseIPv4Number(last)
	return ok && (len(last) >= 2 && (last[:2] == "0x" || last[:2] == "0X"))
}

// parseWHATWGIPv4 对应 IPv4 parser；host 必须已通过 endsInANumber。
func parseWHATWGIPv4(host string) (netip.Addr, bool) {
	parts := strings.Split(host, ".")
	if parts[len(parts)-1] == "" && len(parts) > 1 {
		parts = parts[:len(parts)-1]
	}
	if len(parts) > 4 {
		return netip.Addr{}, false
	}
	nums := make([]uint64, 0, 4)
	for _, p := range parts {
		n, ok := parseIPv4Number(p)
		if !ok {
			return netip.Addr{}, false
		}
		nums = append(nums, n)
	}
	for _, n := range nums[:len(nums)-1] {
		if n > 255 {
			return netip.Addr{}, false
		}
	}
	last := nums[len(nums)-1]
	limit := uint64(1) << (8 * (5 - len(nums)))
	if last >= limit {
		return netip.Addr{}, false
	}
	ipv4 := last
	for i, n := range nums[:len(nums)-1] {
		ipv4 += n << (8 * (3 - i))
	}
	return netip.AddrFrom4([4]byte{byte(ipv4 >> 24), byte(ipv4 >> 16), byte(ipv4 >> 8), byte(ipv4)}), true
}

// serializeIPv6 对应 WHATWG 的 IPv6 serializer：纯十六进制（映射地址也不写成点分），最长的零段压缩成 `::`。
func serializeIPv6(a netip.Addr) string {
	b := a.As16()
	var pieces [8]uint16
	for i := range pieces {
		pieces[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
	}
	bestStart, bestLen := -1, 0
	for i := 0; i < 8; {
		if pieces[i] != 0 {
			i++
			continue
		}
		j := i
		for j < 8 && pieces[j] == 0 {
			j++
		}
		if j-i > bestLen {
			bestStart, bestLen = i, j-i
		}
		i = j
	}
	if bestLen < 2 {
		bestStart = -1
	}
	var sb strings.Builder
	ignore0 := false
	for i := 0; i < 8; i++ {
		if ignore0 && pieces[i] == 0 {
			continue
		}
		ignore0 = false
		if i == bestStart {
			if i == 0 {
				sb.WriteString("::")
			} else {
				sb.WriteString(":")
			}
			ignore0 = true
			continue
		}
		sb.WriteString(strconv.FormatUint(uint64(pieces[i]), 16))
		if i != 7 {
			sb.WriteString(":")
		}
	}
	return sb.String()
}

var schemeRe = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9+.\-]*):`)

// parseEndpointURL 近似 `new URL(raw)`，只覆盖守卫用到的字段；失败返回 error。
// http/https 的权限部分（userinfo、主机、端口）自己切分，不经 net/url——后者对 `http:/x`、`http:///x`、
// 主机里的百分号转义、数字主机的处理都与 WHATWG 不同。
func parseEndpointURL(raw string) (*endpointURL, error) {
	s := strings.TrimFunc(raw, func(r rune) bool { return r <= 0x20 })
	s = strings.NewReplacer("\t", "", "\n", "", "\r", "").Replace(s)
	invalid := fmt.Errorf("Invalid URL")
	m := schemeRe.FindStringSubmatch(s)
	if m == nil {
		return nil, invalid
	}
	scheme := strings.ToLower(m[1])
	out := &endpointURL{Protocol: scheme + ":"}
	if scheme != "http" && scheme != "https" {
		// 非 http(s) 只需要协议名即可被守卫拒绝
		return out, nil
	}
	rest := s[len(m[0]):]
	// 特殊协议：冒号后的任意个 / 或 \ 都当作 `//`
	rest = strings.TrimLeft(rest, `/\`)
	end := strings.IndexAny(rest, `/\?#`)
	if end < 0 {
		end = len(rest)
	}
	authority := rest[:end]
	if at := strings.LastIndex(authority, "@"); at >= 0 {
		authority = authority[at+1:]
	}
	var hostPart, port string
	if strings.HasPrefix(authority, "[") {
		close := strings.Index(authority, "]")
		if close < 0 {
			return nil, invalid
		}
		hostPart = authority[:close+1]
		tail := authority[close+1:]
		if tail != "" {
			if tail[0] != ':' {
				return nil, invalid
			}
			port = tail[1:]
		}
	} else if i := strings.LastIndex(authority, ":"); i >= 0 {
		hostPart, port = authority[:i], authority[i+1:]
	} else {
		hostPart = authority
	}
	if hostPart == "" {
		return nil, invalid
	}
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 0 || n > 65535 || strings.Trim(port, "0123456789") != "" {
			return nil, invalid
		}
		if (scheme == "http" && n == 80) || (scheme == "https" && n == 443) {
			port = ""
		} else {
			port = strconv.Itoa(n)
		}
	}
	var hostText string
	if strings.HasPrefix(hostPart, "[") {
		inner := hostPart[1 : len(hostPart)-1]
		if strings.Contains(inner, "%") {
			return nil, invalid
		}
		a, err := netip.ParseAddr(inner)
		if err != nil || !a.Is6() {
			return nil, invalid
		}
		out.IP, out.IsIP = a, true
		hostText = "[" + serializeIPv6(a) + "]"
	} else {
		decoded, err := url.PathUnescape(hostPart)
		if err != nil {
			return nil, invalid
		}
		host := strings.ToLower(decoded)
		if host == "" || strings.ContainsAny(host, forbiddenHostChars) {
			return nil, invalid
		}
		if endsInANumber(host) {
			a, ok := parseWHATWGIPv4(host)
			if !ok {
				return nil, invalid
			}
			out.IP, out.IsIP = a, true
			hostText = a.String()
		} else {
			hostText = host
		}
	}
	out.Hostname = hostText
	out.Host = hostText
	if port != "" {
		out.Host += ":" + port
	}
	return out, nil
}

// decodeEmbeddedIPv4 对应 net-safety.ts 的 decodeEmbeddedIPv4(ALL_EMBEDDED_PREFIXES)：
// 映射 ::ffff:a.b.c.d、翻译 ::ffff:0:a.b.c.d、兼容 ::a.b.c.d 三种前缀里内嵌的 IPv4。
func decodeEmbeddedIPv4(b [16]byte) (netip.Addr, bool) {
	zero := func(lo, hi int) bool {
		for _, v := range b[lo:hi] {
			if v != 0 {
				return false
			}
		}
		return true
	}
	mapped := zero(0, 10) && b[10] == 0xff && b[11] == 0xff
	translated := zero(0, 8) && b[8] == 0xff && b[9] == 0xff && b[10] == 0 && b[11] == 0
	compatible := zero(0, 12)
	if !mapped && !translated && !compatible {
		return netip.Addr{}, false
	}
	// `::` 与 `::1` 是未指定地址与回环，不是内嵌 IPv4（否则回环会被解成 0.0.0.1 而被禁，本地 Ollama 连不上）
	if compatible && !mapped && !translated && b[12] == 0 && b[13] == 0 {
		return netip.Addr{}, false
	}
	return netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]}), true
}

// isForbiddenAIEndpointAddr 对应 isForbiddenAIEndpointAddress：
// 只挡没有合法 AI 用途的目标——link-local（含云元数据）、组播、保留段、0.0.0.0/8。回环与局域网放行。
func isForbiddenAIEndpointAddr(a netip.Addr) bool {
	a = a.WithZone("")
	if a.Is4() {
		b := a.As4()
		return (b[0] == 169 && b[1] == 254) || b[0] >= 224 || b[0] == 0
	}
	b := a.As16()
	if v4, ok := decodeEmbeddedIPv4(b); ok {
		return isForbiddenAIEndpointAddr(v4)
	}
	first := uint16(b[0])<<8 | uint16(b[1])
	return first&0xffc0 == 0xfe80 || first&0xff00 == 0xff00
}

// ---- 端点准入（evaluateEndpointAccess） ----

const unknownHostIntervalMs = 10_000

type gateDecision struct {
	Allowed bool
	Reason  string
	Host    string // 空表示无法解析
	IsKnown bool
}

// normalizeEndpointHost 对应 normalizeHost：`${protocol}//${host}` 小写。
func normalizeEndpointHost(raw string) (string, bool) {
	u, err := parseEndpointURL(raw)
	if err != nil {
		return "", false
	}
	return strings.ToLower(u.Protocol + "//" + u.Host), true
}

// isLoopbackHostname 对应 isLoopbackHost。注意 `/^127\./` 对任何以 "127." 开头的主机名成立，
// 包括 127.evil.com 这种域名——这是 TS 原样的行为，这里保持一致，在迁移文档里单独记录。
func isLoopbackHostname(hostname string) bool {
	h := strings.ToLower(hostname)
	h = strings.TrimPrefix(h, "[")
	h = strings.TrimSuffix(h, "]")
	return h == "localhost" || h == "::1" || h == "0:0:0:0:0:0:0:1" || strings.HasPrefix(h, "127.")
}

func evaluateEndpointAccess(rawURL string, nowMs int64, configured, granted map[string]bool, lastUnknownAt int64) gateDecision {
	host, ok := normalizeEndpointHost(rawURL)
	if !ok {
		return gateDecision{Reason: "无效的接口地址"}
	}
	hostname := ""
	if u, err := parseEndpointURL(rawURL); err == nil {
		hostname = u.Hostname
	}
	if configured[host] || granted[host] || isLoopbackHostname(hostname) {
		return gateDecision{Allowed: true, Host: host, IsKnown: true}
	}
	if elapsed := nowMs - lastUnknownAt; elapsed < unknownHostIntervalMs {
		wait := (unknownHostIntervalMs - elapsed + 999) / 1000
		return gateDecision{Reason: fmt.Sprintf("未配置的接口地址请求过于频繁，请 %d 秒后重试", wait), Host: host}
	}
	return gateDecision{Allowed: true, Host: host}
}
