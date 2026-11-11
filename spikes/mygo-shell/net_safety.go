package main

// 对应 main/services/net-safety.ts：DNS 解析 + 私网地址检测（SSRF 防护）。
// AI 端点的宽松版 isForbiddenAIEndpointAddr 在 ai_net.go，两者共用 decodeEmbeddedIPv4。
//
// 与 TS 版的差别：
//   - 以 netip.Addr 的 16 字节判定，而不是对字符串做 split/parseInt；点分写法的兼容地址
//     （::127.0.0.1）TS 会因解析成 0x127 而漏判，这里按真实字节判为私网（更严）。
//   - DNS 失败消息沿用 Node 的 getaddrinfo ENOTFOUND 形态，不保证与 Go 的原始错误文本一致。

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
)

type resolvedAddress struct {
	Address string
	Family  int // 4 | 6
}

type resolveOptions struct {
	AllowPrivateNetwork bool
	// 允许代理（Clash/Mihomo TUN fake-ip）的 198.18/15 合成应答
	AllowProxyCompatibilityAddress bool
}

// lookupFunc 是 dns.lookup(all, verbatim) 的替身，测试里可注入。
type lookupFunc func(ctx context.Context, host string) ([]netip.Addr, error)

func systemLookup(ctx context.Context, host string) ([]netip.Addr, error) {
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return nil, fmt.Errorf("getaddrinfo ENOTFOUND %s", host)
		}
		return nil, err
	}
	return addrs, nil
}

func isBlockedHostname(hostname string) bool {
	h := strings.ToLower(hostname)
	return h == "localhost" || strings.HasSuffix(h, ".localhost") ||
		h == "localhost.localdomain" || strings.HasSuffix(h, ".localdomain")
}

func isPrivateIPv4Bytes(b [4]byte) bool {
	a, c, d := b[0], b[1], b[2]
	return a == 0 || a == 10 || a == 127 ||
		(a == 169 && c == 254) ||
		(a == 172 && c >= 16 && c <= 31) ||
		(a == 192 && c == 168) ||
		(a == 100 && c >= 64 && c <= 127) || // CGNAT
		(a >= 224 && a <= 239) || // 组播
		a >= 240 || // 保留
		(a == 198 && (c == 18 || c == 19)) || // 基准测试
		(a == 192 && c == 0 && d == 2) || // TEST-NET-1/2/3
		(a == 198 && c == 51 && d == 100) ||
		(a == 203 && c == 0 && d == 113)
}

func isPrivateIPv6Bytes(b [16]byte) bool {
	allZero := true
	for _, v := range b[:15] {
		if v != 0 {
			allZero = false
			break
		}
	}
	if allZero && (b[15] == 0 || b[15] == 1) { // :: 与 ::1
		return true
	}
	if v4, ok := decodeEmbeddedIPv4(b); ok {
		return isPrivateIPv4Bytes(v4.As4())
	}
	first := uint16(b[0])<<8 | uint16(b[1])
	second := uint16(b[2])<<8 | uint16(b[3])
	return first&0xfe00 == 0xfc00 || // ULA
		first&0xff00 == 0xff00 || // 组播
		first&0xffc0 == 0xfe80 || // link-local
		first == 0x2002 || // 6to4
		(first == 0x2001 && second == 0x0000) || // Teredo
		(first == 0x2001 && second == 0x0db8) || // 文档段
		first == 0x0100 || // discard
		(first == 0x0064 && second == 0xff9b) // NAT64
}

// isPrivateAddr 对应 isPrivateAddress（输入已是合法 IP）。
func isPrivateAddr(a netip.Addr) bool {
	a = a.WithZone("")
	if a.Is4() {
		return isPrivateIPv4Bytes(a.As4())
	}
	return isPrivateIPv6Bytes(a.As16())
}

// parseIPLiteral 对应 nodeNet.isIP：返回地址与族（0 表示不是 IP 字面量）。
func parseIPLiteral(s string) (netip.Addr, int) {
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}, 0
	}
	if a.Is4() {
		return a, 4
	}
	return a, 6
}

// isPrivateAddress 对应 isPrivateAddress(string)：非 IP 字面量返回 false。
func isPrivateAddress(s string) bool {
	a, fam := parseIPLiteral(s)
	return fam != 0 && isPrivateAddr(a)
}

// isProxyCompatibilityAddr：198.18.0.0/15，或映射/翻译写法内嵌的同一段。
func isProxyCompatibilityAddr(a netip.Addr) bool {
	a = a.WithZone("")
	if a.Is4() {
		b := a.As4()
		return b[0] == 198 && (b[1] == 18 || b[1] == 19)
	}
	b := a.As16()
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
	if !mapped && !translated {
		return false
	}
	return isProxyCompatibilityAddr(netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]}))
}

// resolvePublicAddress 对应 resolvePublicAddress：校验主机名并返回要连接的地址。
func resolvePublicAddress(ctx context.Context, hostname string, opts resolveOptions, lookup lookupFunc) (resolvedAddress, error) {
	if isBlockedHostname(hostname) {
		return resolvedAddress{}, errors.New("Access to local network addresses is not allowed")
	}
	if lookup == nil {
		lookup = systemLookup
	}
	if a, fam := parseIPLiteral(hostname); fam != 0 {
		if isPrivateAddr(a) {
			if opts.AllowPrivateNetwork || (isProxyCompatibilityAddr(a) && opts.AllowProxyCompatibilityAddress) {
				return resolvedAddress{Address: hostname, Family: fam}, nil
			}
			return resolvedAddress{}, errors.New("Access to internal network addresses is not allowed")
		}
		return resolvedAddress{Address: hostname, Family: fam}, nil
	}

	addrs, err := lookup(ctx, hostname)
	if err != nil {
		return resolvedAddress{}, err
	}
	if len(addrs) == 0 {
		return resolvedAddress{}, errors.New("Failed to resolve remote host")
	}
	family := func(a netip.Addr) int {
		if a.Is4() {
			return 4
		}
		return 6
	}
	var trusted []netip.Addr
	for _, a := range addrs {
		if isProxyCompatibilityAddr(a) {
			trusted = append(trusted, a)
		}
	}
	if len(trusted) > 0 && len(trusted) == len(addrs) && opts.AllowProxyCompatibilityAddress {
		return resolvedAddress{Address: trusted[0].String(), Family: family(trusted[0])}, nil
	}
	if !opts.AllowPrivateNetwork {
		for _, a := range addrs {
			if !isPrivateAddr(a) {
				continue
			}
			if len(trusted) == len(addrs) {
				return resolvedAddress{}, fmt.Errorf("「%s」解析到 %s，这是 Clash、Mihomo 等代理在 TUN（fake-ip）模式下返回的虚拟地址，未经代理直连会被内网访问防护拦截。请在「设置 → 网络设置」中配置代理，或开启系统代理后重试。", hostname, addrs[0])
			}
			return resolvedAddress{}, errors.New("Access to internal network addresses is not allowed")
		}
	}
	return resolvedAddress{Address: addrs[0].String(), Family: family(addrs[0])}, nil
}
