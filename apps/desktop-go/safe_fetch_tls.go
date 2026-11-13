package main

// 抓取层的 HTTPS 出站：TLS 指纹与 Electron 主进程（TS 版 safe-fetch）一致，并自行处理代理隧道。
//
// 为什么：Go 标准库的 ClientHello（JA3 9b7dcdf3…）会被一部分 WAF 判为非浏览器。实测 img.nga.cn（腾讯 EdgeOne）
// 对它一律返回 HTTP 567，而同一张图在 Electron/Node 指纹（JA3 5355e385…）下返回 200；请求头、Connection
// 完全相同也一样，只换 ClientHello 就恢复（verify/nga-ab/imglab 的 v0–v3 四个变体，见迁移文档）。
// 这里让 Go 版与现有 Electron 版的线上表现保持一致，不是新增能力。
//
// 做法：用 utls 按内置样本（tls_hello_electron.hex，取自 Electron 45.2.3 / Node 24.22.2 实际发出的
// ClientHello，由 verify/wire/wire-compare.mts 捕获）握手；只发 HTTP/1.1（样本无 ALPN），证书与主机名照常校验。
// Go 的 Transport 对「经代理的 HTTPS」不走自定义 TLS 拨号，所以 HTTPS 目标的代理隧道（HTTP CONNECT / SOCKS5）
// 在这里自己建，域名仍由代理解析。IP 字面量主机不发 SNI，改用标准库握手。

import (
	"bufio"
	"context"
	"crypto/tls"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	utls "github.com/refraction-networking/utls"
	xproxy "golang.org/x/net/proxy"
)

//go:embed tls_hello_electron.hex
var electronHelloHex string

// electronClientHelloSpec 每次返回一份新的规格（ApplyPreset 会改写它）。
func electronClientHelloSpec() (*utls.ClientHelloSpec, error) {
	raw, err := hex.DecodeString(strings.TrimSpace(electronHelloHex))
	if err != nil {
		return nil, fmt.Errorf("内置 ClientHello 样本损坏：%w", err)
	}
	spec, err := (&utls.Fingerprinter{AllowBluntMimicry: true}).FingerprintClientHello(raw)
	if err != nil {
		return nil, fmt.Errorf("解析内置 ClientHello 样本失败：%w", err)
	}
	// 样本里的密钥共享是当时对端协商用的公钥数据，我们没有对应私钥：清空后由 utls 现生成同组密钥。
	for _, e := range spec.Extensions {
		if ks, ok := e.(*utls.KeyShareExtension); ok {
			for i := range ks.KeyShares {
				ks.KeyShares[i].Data = nil
			}
		}
	}
	return spec, nil
}

// dialRaw 拨号到 addr 并套上空闲超时；测试可经 dialOverride 替换。
func (f *safeFetcher) dialRaw(ctx context.Context, network, addr string) (net.Conn, error) {
	dial := (&net.Dialer{Timeout: f.idle}).DialContext
	if f.dialOverride != nil {
		dial = f.dialOverride
	}
	c, err := dial(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	return idleConn{Conn: c, d: f.idle}, nil
}

// handshakeTLS 在 raw 上完成 TLS 握手：域名用 Electron 指纹，IP 字面量用标准库。
func (f *safeFetcher) handshakeTLS(ctx context.Context, raw net.Conn, serverName string) (net.Conn, error) {
	hctx, cancel := context.WithTimeout(ctx, f.idle)
	defer cancel()
	if net.ParseIP(serverName) != nil {
		c := tls.Client(raw, &tls.Config{ServerName: serverName, RootCAs: f.tlsRoots})
		if err := c.HandshakeContext(hctx); err != nil {
			_ = raw.Close()
			return nil, err
		}
		return c, nil
	}
	spec, err := electronClientHelloSpec()
	if err != nil {
		_ = raw.Close()
		return nil, err
	}
	uc := utls.UClient(raw, &utls.Config{ServerName: serverName, RootCAs: f.tlsRoots}, utls.HelloCustom)
	if err := uc.ApplyPreset(spec); err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("应用 ClientHello 样本失败：%w", err)
	}
	if err := uc.HandshakeContext(hctx); err != nil {
		_ = raw.Close()
		return nil, err
	}
	return uc, nil
}

// dialTLS 返回 http.Transport.DialTLSContext：直连时钉扎到已校验的 IP，走代理时先建隧道，再握手。
func (f *safeFetcher) dialTLS(target *url.URL, pinned *resolvedAddress, viaProxy bool) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		var raw net.Conn
		if proxyURL := f.proxy.ProxyFor(target); viaProxy && proxyURL != nil {
			raw, err = f.dialProxyTunnel(ctx, proxyURL, addr)
		} else {
			dialAddr := addr
			if pinned != nil {
				// 钉扎到刚校验过的 IP，堵住「校验后 DNS 再变」的 rebinding 窗口；SNI 与 Host 头仍用原域名
				dialAddr = net.JoinHostPort(pinned.Address, port)
			}
			raw, err = f.dialRaw(ctx, network, dialAddr)
		}
		if err != nil {
			return nil, err
		}
		return f.handshakeTLS(ctx, raw, host)
	}
}

type dialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

func (d dialFunc) Dial(network, addr string) (net.Conn, error) {
	return d(context.Background(), network, addr)
}
func (d dialFunc) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return d(ctx, network, addr)
}

// bufferedConn 把 CONNECT 响应之后已被 bufio 读走的字节还给上层。
type bufferedConn struct {
	net.Conn
	br *bufio.Reader
}

func (c bufferedConn) Read(p []byte) (int, error) { return c.br.Read(p) }

// dialProxyTunnel 经代理建到 targetAddr（host:port）的 TCP 隧道。目标域名交给代理解析。
func (f *safeFetcher) dialProxyTunnel(ctx context.Context, p *url.URL, targetAddr string) (net.Conn, error) {
	proxyAddr := p.Host
	if p.Port() == "" {
		def := map[string]string{"http": "80", "https": "443", "socks5": "1080", "socks5h": "1080"}[p.Scheme]
		proxyAddr = net.JoinHostPort(p.Hostname(), def)
	}
	switch p.Scheme {
	case "socks5", "socks5h":
		var auth *xproxy.Auth
		if p.User != nil {
			pw, _ := p.User.Password()
			auth = &xproxy.Auth{User: p.User.Username(), Password: pw}
		}
		d, err := xproxy.SOCKS5("tcp", proxyAddr, auth, dialFunc(f.dialRaw))
		if err != nil {
			return nil, err
		}
		c, err := d.(xproxy.ContextDialer).DialContext(ctx, "tcp", targetAddr)
		if err != nil {
			return nil, fmt.Errorf("代理隧道失败：%w", err)
		}
		return c, nil
	case "http", "https":
		raw, err := f.dialRaw(ctx, "tcp", proxyAddr)
		if err != nil {
			return nil, err
		}
		conn := raw
		if p.Scheme == "https" {
			tc := tls.Client(raw, &tls.Config{ServerName: p.Hostname(), RootCAs: f.tlsRoots})
			hctx, cancel := context.WithTimeout(ctx, f.idle)
			defer cancel()
			if err := tc.HandshakeContext(hctx); err != nil {
				_ = raw.Close()
				return nil, fmt.Errorf("代理隧道失败：%w", err)
			}
			conn = tc
		}
		req := "CONNECT " + targetAddr + " HTTP/1.1\r\nHost: " + targetAddr + "\r\n"
		if p.User != nil {
			pw, _ := p.User.Password()
			req += "Proxy-Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(p.User.Username()+":"+pw)) + "\r\n"
		}
		if _, err := conn.Write([]byte(req + "\r\n")); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("代理隧道失败：%w", err)
		}
		br := bufio.NewReader(conn)
		resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("代理隧道失败：%w", err)
		}
		if resp.StatusCode != http.StatusOK {
			_ = conn.Close()
			return nil, fmt.Errorf("代理隧道失败：%s", resp.Status)
		}
		if br.Buffered() > 0 {
			return bufferedConn{Conn: conn, br: br}, nil
		}
		return conn, nil
	}
	return nil, fmt.Errorf("不支持的代理协议：%s", p.Scheme)
}
