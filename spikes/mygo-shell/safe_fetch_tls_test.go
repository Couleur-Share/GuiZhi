package main

// 抓取层 HTTPS 的 TLS 指纹：与 Electron 主进程（TS 版）发出的 ClientHello 一致。
// 缘由：Go 默认指纹（JA3 9b7dcdf3…）会被 img.nga.cn 前的腾讯 EdgeOne 判为非浏览器并返回 HTTP 567，
// 而 TS 版指纹（JA3 5355e385…）正常；实测见 verify/nga-ab/imglab 与 MYGO-MIGRATION.md。
// 这里全部只连本机，不访问真实站点。

import (
	"bufio"
	"context"
	"crypto/md5"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"
)

func tlsTestIsGrease(v uint16) bool { return v&0x0f0f == 0x0a0a }

// tlsTestJA3 从 TLS 记录层字节解出 JA3 串（忽略 GREASE）。
func tlsTestJA3(rec []byte) (string, error) {
	if len(rec) < 6 || rec[0] != 0x16 || rec[5] != 0x01 {
		return "", fmt.Errorf("不是 ClientHello 记录")
	}
	p := 9
	ver := binary.BigEndian.Uint16(rec[p:])
	p += 2 + 32
	p += 1 + int(rec[p])
	csLen := int(binary.BigEndian.Uint16(rec[p:]))
	p += 2
	var ciphers []uint16
	for i := 0; i < csLen; i += 2 {
		ciphers = append(ciphers, binary.BigEndian.Uint16(rec[p+i:]))
	}
	p += csLen
	p += 1 + int(rec[p])
	end := p + 2 + int(binary.BigEndian.Uint16(rec[p:]))
	p += 2
	var exts, curves []uint16
	var points []byte
	for p+4 <= end {
		typ := binary.BigEndian.Uint16(rec[p:])
		l := int(binary.BigEndian.Uint16(rec[p+2:]))
		body := rec[p+4 : p+4+l]
		exts = append(exts, typ)
		switch typ {
		case 10:
			n := int(binary.BigEndian.Uint16(body))
			for i := 0; i < n; i += 2 {
				curves = append(curves, binary.BigEndian.Uint16(body[2+i:]))
			}
		case 11:
			points = body[1 : 1+int(body[0])]
		}
		p += 4 + l
	}
	join := func(a []uint16) string {
		var s []string
		for _, x := range a {
			if !tlsTestIsGrease(x) {
				s = append(s, fmt.Sprint(x))
			}
		}
		return strings.Join(s, "-")
	}
	var pts []string
	for _, b := range points {
		pts = append(pts, fmt.Sprint(b))
	}
	return fmt.Sprintf("%d,%s,%s,%s,%s", ver, join(ciphers), join(exts), join(curves), strings.Join(pts, "-")), nil
}

func tlsTestFetcher(dial func(ctx context.Context, network, addr string) (net.Conn, error)) *safeFetcher {
	return &safeFetcher{
		proxy: newNetProxyState(),
		lookup: func(context.Context, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
		},
		dialOverride: dial,
		idle:         5 * time.Second,
	}
}

func dialTo(addr string) func(ctx context.Context, network, _ string) (net.Conn, error) {
	return func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
}

// 抓取层发出的 ClientHello 必须与内置的 Electron 样本 JA3 完全一致，并带正确的 SNI、不带 ALPN。
func TestSafeFetchHTTPSClientHelloMatchesElectron(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan []byte, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
		buf := make([]byte, 16<<10)
		n, _ := c.Read(buf)
		got <- buf[:n]
	}()
	f := tlsTestFetcher(dialTo(ln.Addr().String()))
	_, _ = f.fetchRawText(context.Background(), "https://img.nga.cn/attachments/a.jpg", safeRequestOptions{}, 1024)
	var hello []byte
	select {
	case hello = <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("没有收到 ClientHello")
	}
	want, err := hex.DecodeString(strings.TrimSpace(electronHelloHex))
	if err != nil {
		t.Fatal(err)
	}
	wantJA3, err := tlsTestJA3(want)
	if err != nil {
		t.Fatal(err)
	}
	gotJA3, err := tlsTestJA3(hello)
	if err != nil {
		t.Fatalf("解析抓取层发出的 ClientHello 失败：%v（%d 字节）", err, len(hello))
	}
	if gotJA3 != wantJA3 {
		t.Errorf("JA3 不一致\n  期望 %x %s\n  实际 %x %s", md5.Sum([]byte(wantJA3)), wantJA3, md5.Sum([]byte(gotJA3)), gotJA3)
	}
	if !strings.Contains(string(hello), "img.nga.cn") {
		t.Errorf("ClientHello 缺少 SNI img.nga.cn")
	}
}

func newTLSTestServer(t *testing.T) (*httptest.Server, *x509.CertPool) {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "ok host=%s", r.Host)
	}))
	srv.EnableHTTP2 = false
	srv.StartTLS()
	t.Cleanup(srv.Close)
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	return srv, pool
}

func tlsTestPort(srv *httptest.Server) string {
	_, p, _ := net.SplitHostPort(srv.Listener.Addr().String())
	return p
}

// httptest 证书只对 example.com / 127.0.0.1 有效：证书链与主机名都必须真的被校验。
func TestSafeFetchHTTPSVerifiesCertificate(t *testing.T) {
	srv, pool := newTLSTestServer(t)
	port := tlsTestPort(srv)
	ctx := context.Background()

	f := tlsTestFetcher(dialTo(srv.Listener.Addr().String()))
	f.tlsRoots = pool
	res, err := f.fetchRawText(ctx, "https://example.com:"+port+"/x", safeRequestOptions{}, 1024)
	if err != nil {
		t.Fatalf("受信证书应成功：%v", err)
	}
	if res.Status != 200 || !strings.HasPrefix(res.Text, "ok host=example.com:"+port) {
		t.Errorf("结果不符：%+v", res)
	}

	if _, err := f.fetchRawText(ctx, "https://other.test:"+port+"/x", safeRequestOptions{}, 1024); err == nil {
		t.Error("证书主机名不符应失败")
	}

	untrusted := tlsTestFetcher(dialTo(srv.Listener.Addr().String())) // 不信任测试根证书
	if _, err := untrusted.fetchRawText(ctx, "https://example.com:"+port+"/x", safeRequestOptions{}, 1024); err == nil {
		t.Error("未受信的证书应失败")
	}
}

// ---- 代理隧道：HTTPS 目标经 HTTP CONNECT / SOCKS5 代理时同样使用该指纹，域名由代理解析 ----

type tunnelRecorder struct {
	mu      sync.Mutex
	targets []string
	auth    []string
}

func (r *tunnelRecorder) add(target, auth string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.targets = append(r.targets, target)
	r.auth = append(r.auth, auth)
}

func pipeBoth(a, b net.Conn) {
	done := make(chan struct{}, 2)
	cp := func(dst, src net.Conn) { _, _ = io.Copy(dst, src); _ = dst.Close(); done <- struct{}{} }
	go cp(a, b)
	go cp(b, a)
	<-done
	<-done
}

func startConnectProxy(t *testing.T, upstream string, rec *tunnelRecorder) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				br := bufio.NewReader(c)
				req, err := http.ReadRequest(br)
				if err != nil || req.Method != http.MethodConnect {
					c.Close()
					return
				}
				rec.add(req.Host, req.Header.Get("Proxy-Authorization"))
				up, err := net.Dial("tcp", upstream)
				if err != nil {
					_, _ = io.WriteString(c, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
					c.Close()
					return
				}
				_, _ = io.WriteString(c, "HTTP/1.1 200 Connection Established\r\n\r\n")
				pipeBoth(c, up)
			}()
		}
	}()
	return ln.Addr().String()
}

func startSocks5Proxy(t *testing.T, upstream string, rec *tunnelRecorder) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				hdr := make([]byte, 2)
				if _, err := io.ReadFull(c, hdr); err != nil {
					c.Close()
					return
				}
				if _, err := io.ReadFull(c, make([]byte, hdr[1])); err != nil {
					c.Close()
					return
				}
				_, _ = c.Write([]byte{5, 0})
				req := make([]byte, 4)
				if _, err := io.ReadFull(c, req); err != nil {
					c.Close()
					return
				}
				var host string
				switch req[3] {
				case 3:
					l := make([]byte, 1)
					_, _ = io.ReadFull(c, l)
					b := make([]byte, l[0])
					_, _ = io.ReadFull(c, b)
					host = string(b)
				case 1:
					b := make([]byte, 4)
					_, _ = io.ReadFull(c, b)
					host = net.IP(b).String()
				default:
					c.Close()
					return
				}
				pb := make([]byte, 2)
				_, _ = io.ReadFull(c, pb)
				rec.add(fmt.Sprintf("%s:%d", host, binary.BigEndian.Uint16(pb)), "")
				up, err := net.Dial("tcp", upstream)
				if err != nil {
					_, _ = c.Write([]byte{5, 1, 0, 1, 0, 0, 0, 0, 0, 0})
					c.Close()
					return
				}
				_, _ = c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
				pipeBoth(c, up)
			}()
		}
	}()
	return ln.Addr().String()
}

func proxiedTLSFetcher(t *testing.T, pool *x509.CertPool, protocol, proxyAddr string) *safeFetcher {
	t.Helper()
	host, portStr, _ := net.SplitHostPort(proxyAddr)
	var port int
	fmt.Sscanf(portStr, "%d", &port)
	f := &safeFetcher{
		proxy: newNetProxyState(),
		// 走代理时域名交给代理解析：本地解析被调用就是缺陷
		lookup: func(_ context.Context, h string) ([]netip.Addr, error) {
			t.Errorf("走代理时不应在本地解析域名 %s", h)
			return nil, fmt.Errorf("不应解析")
		},
		tlsRoots: pool,
		idle:     5 * time.Second,
	}
	f.proxy.Apply(map[string]any{"mode": "manual", "protocol": protocol, "host": host, "port": float64(port)})
	return f
}

func TestSafeFetchHTTPSThroughHTTPProxy(t *testing.T) {
	srv, pool := newTLSTestServer(t)
	rec := &tunnelRecorder{}
	f := proxiedTLSFetcher(t, pool, "http", startConnectProxy(t, srv.Listener.Addr().String(), rec))
	res, err := f.fetchRawText(context.Background(), "https://example.com:"+tlsTestPort(srv)+"/x", safeRequestOptions{}, 1024)
	if err != nil {
		t.Fatalf("经 HTTP 代理失败：%v", err)
	}
	if res.Status != 200 || !strings.HasPrefix(res.Text, "ok host=example.com:") {
		t.Errorf("结果不符：%+v", res)
	}
	if len(rec.targets) != 1 || rec.targets[0] != "example.com:"+tlsTestPort(srv) {
		t.Errorf("CONNECT 目标应为域名而非本地解析的 IP：%v", rec.targets)
	}
}

func TestSafeFetchHTTPSThroughSocks5Proxy(t *testing.T) {
	srv, pool := newTLSTestServer(t)
	rec := &tunnelRecorder{}
	f := proxiedTLSFetcher(t, pool, "socks5", startSocks5Proxy(t, srv.Listener.Addr().String(), rec))
	res, err := f.fetchRawText(context.Background(), "https://example.com:"+tlsTestPort(srv)+"/x", safeRequestOptions{}, 1024)
	if err != nil {
		t.Fatalf("经 SOCKS5 代理失败：%v", err)
	}
	if res.Status != 200 || !strings.HasPrefix(res.Text, "ok host=example.com:") {
		t.Errorf("结果不符：%+v", res)
	}
	if len(rec.targets) != 1 || rec.targets[0] != "example.com:"+tlsTestPort(srv) {
		t.Errorf("SOCKS5 目标应为域名：%v", rec.targets)
	}
}

// 代理拒绝隧道（非 200）要给出可读错误，而不是在握手阶段报莫名其妙的 EOF。
func TestSafeFetchHTTPSProxyTunnelRefused(t *testing.T) {
	_, pool := newTLSTestServer(t)
	rec := &tunnelRecorder{}
	f := proxiedTLSFetcher(t, pool, "http", startConnectProxy(t, "127.0.0.1:1", rec)) // 上游不可达 → 502
	_, err := f.fetchRawText(context.Background(), "https://example.com/x", safeRequestOptions{}, 1024)
	if err == nil || !strings.Contains(err.Error(), "代理") {
		t.Fatalf("应报代理隧道失败，实际：%v", err)
	}
}
