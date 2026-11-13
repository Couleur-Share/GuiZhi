// imglab：定位「Go 版下载 img.nga.cn 附件图被判 HTTP 567、Electron 版正常」的原因。
// 独立模块（自带 go.mod，不污染 mygo-shell 的依赖）。每个变体只改一个因素，请求头沿用产品的 NGA 图片请求：
//
//	v0  net/http，与产品抓取层一致（DisableKeepAlives，线上带 Connection: close）            —— 基线（已知 567）
//	v1  net/http，开启连接复用并显式发 Connection: keep-alive                                  —— 只改 Connection
//	v2  utls 复刻 Electron 的 ClientHello（取自 wire-compare 捕获的真实字节），Connection: close —— 只改 TLS 指纹
//	v3  utls 复刻 Electron 的 ClientHello，Connection: keep-alive                              —— TLS 指纹 + Connection 都改
//
// 用法：
//
//	imglab -hello <clienthello-ts-electron.hex> -ja3                       离线：核对 utls 发出的 JA3 与 Electron 一致（只连本机）
//	imglab -hello <hex> -dry                                               离线：对本机 TLS 服务器走通 4 个变体
//	imglab -hello <hex> -url <图片地址> -variants v0,v1,v2,v3 -n 2 -interval 3s   真实：每个变体请求 n 次（需用户批准）
package main

import (
	"bufio"
	"context"
	"crypto/md5"
	"crypto/tls"
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"time"

	utls "github.com/refraction-networking/utls"
)

const (
	ua      = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	referer = "https://bbs.nga.cn/"
)

// ---- JA3 ----

func isGrease(v uint16) bool { return v&0x0f0f == 0x0a0a }

func ja3Of(rec []byte) (string, error) {
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
	extTotal := int(binary.BigEndian.Uint16(rec[p:]))
	p += 2
	end := p + extTotal
	var exts, curves []uint16
	var points []byte
	for p+4 <= end {
		t := binary.BigEndian.Uint16(rec[p:])
		l := int(binary.BigEndian.Uint16(rec[p+2:]))
		body := rec[p+4 : p+4+l]
		exts = append(exts, t)
		switch t {
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
			if !isGrease(x) {
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

func ja3Hash(s string) string { return fmt.Sprintf("%x", md5.Sum([]byte(s))) }

// ---- utls：从真实 ClientHello 字节复刻 ----

func loadSpec(helloHex string) (*utls.ClientHelloSpec, []byte, error) {
	b, err := os.ReadFile(helloHex)
	if err != nil {
		return nil, nil, err
	}
	raw, err := hex.DecodeString(strings.TrimSpace(string(b)))
	if err != nil {
		return nil, nil, err
	}
	fp := &utls.Fingerprinter{AllowBluntMimicry: true}
	spec, err := fp.FingerprintClientHello(raw)
	if err != nil {
		return nil, nil, err
	}
	// 捕获到的密钥共享里是对方的公钥数据，我们没有对应私钥：清空后由 utls 现生成同组的密钥
	for _, e := range spec.Extensions {
		if ks, ok := e.(*utls.KeyShareExtension); ok {
			for i := range ks.KeyShares {
				ks.KeyShares[i].Data = nil
			}
		}
	}
	return spec, raw, nil
}

type target struct {
	addr     string // 实际拨号地址
	host     string // SNI 与 Host 头
	path     string
	insecure bool // 仅 -dry：本机自签证书
}

// utlsGET 用复刻的 ClientHello 握手，再手写 HTTP/1.1 请求（头顺序与 Node 一致；conn 为 close/keep-alive 可选）。
func utlsGET(ctx context.Context, spec *utls.ClientHelloSpec, t target, connection string) (*http.Response, []byte, error) {
	d := &net.Dialer{Timeout: 15 * time.Second}
	tcp, err := d.DialContext(ctx, "tcp", t.addr)
	if err != nil {
		return nil, nil, err
	}
	defer tcp.Close()
	_ = tcp.SetDeadline(time.Now().Add(30 * time.Second))
	uc := utls.UClient(tcp, &utls.Config{ServerName: t.host, InsecureSkipVerify: t.insecure}, utls.HelloCustom)
	// ApplyPreset 会改写 spec，每次请求用一份拷贝，避免互相影响
	fresh, _, err := cloneSpec(spec)
	if err != nil {
		return nil, nil, err
	}
	if err := uc.ApplyPreset(fresh); err != nil {
		return nil, nil, fmt.Errorf("ApplyPreset：%w", err)
	}
	if err := uc.HandshakeContext(ctx); err != nil {
		return nil, nil, fmt.Errorf("握手：%w", err)
	}
	req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUser-Agent: %s\r\nAccept: */*\r\nAccept-Language: zh-CN,zh;q=0.9,en;q=0.8\r\nReferer: %s\r\nConnection: %s\r\n\r\n", t.path, t.host, ua, referer, connection)
	if _, err := io.WriteString(uc, req); err != nil {
		return nil, nil, err
	}
	resp, err := http.ReadResponse(bufio.NewReader(uc), nil)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	return resp, body, nil
}

var specSource []byte

func cloneSpec(_ *utls.ClientHelloSpec) (*utls.ClientHelloSpec, []byte, error) {
	fp := &utls.Fingerprinter{AllowBluntMimicry: true}
	spec, err := fp.FingerprintClientHello(specSource)
	if err != nil {
		return nil, nil, err
	}
	for _, e := range spec.Extensions {
		if ks, ok := e.(*utls.KeyShareExtension); ok {
			for i := range ks.KeyShares {
				ks.KeyShares[i].Data = nil
			}
		}
	}
	return spec, specSource, nil
}

// goGET 对应产品抓取层：net/http；keepAlive=true 时开启连接复用并显式发 Connection: keep-alive。
func goGET(ctx context.Context, t target, keepAlive bool) (*http.Response, []byte, error) {
	tr := &http.Transport{
		DisableKeepAlives:  !keepAlive,
		DisableCompression: true,
		TLSClientConfig:    &tls.Config{ServerName: t.host, InsecureSkipVerify: t.insecure},
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, network, t.addr)
		},
		TLSHandshakeTimeout: 15 * time.Second,
	}
	defer tr.CloseIdleConnections()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+t.host+t.path, nil)
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Referer", referer)
	if keepAlive {
		req.Header.Set("Connection", "keep-alive")
	}
	resp, err := (&http.Client{Transport: tr, Timeout: 40 * time.Second}).Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	return resp, body, nil
}

func describe(resp *http.Response, body []byte) string {
	kind := "非图片"
	switch {
	case len(body) >= 3 && body[0] == 0xff && body[1] == 0xd8:
		kind = "JPEG"
	case len(body) >= 8 && string(body[1:4]) == "PNG":
		kind = "PNG"
	case len(body) >= 4 && string(body[:4]) == "GIF8":
		kind = "GIF"
	}
	return fmt.Sprintf("状态 %d  类型 %s  内容识别 %s  首 %d 字节  Server=%q  Set-Cookie 名=%v", resp.StatusCode, resp.Header.Get("Content-Type"), kind, len(body), resp.Header.Get("Server"), cookieNames(resp))
}

func cookieNames(r *http.Response) []string {
	var n []string
	for _, c := range r.Header.Values("Set-Cookie") {
		if k, _, ok := strings.Cut(c, "="); ok {
			n = append(n, k)
		}
	}
	return n
}

func main() {
	helloPath := flag.String("hello", "", "Electron 的 ClientHello 十六进制文件（wire-compare 产出）")
	rawURL := flag.String("url", "", "真实图片地址（真实模式）")
	variants := flag.String("variants", "v0,v1,v2,v3", "要跑的变体")
	n := flag.Int("n", 2, "每个变体请求次数")
	interval := flag.Duration("interval", 3*time.Second, "请求间隔")
	dry := flag.Bool("dry", false, "离线：对本机 TLS 服务器走通各变体")
	ja3 := flag.Bool("ja3", false, "离线：核对 utls 发出的 JA3")
	flag.Parse()
	if *helloPath == "" {
		fmt.Fprintln(os.Stderr, "需要 -hello")
		os.Exit(2)
	}
	spec, raw, err := loadSpec(*helloPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "解析 ClientHello 失败：", err)
		os.Exit(1)
	}
	specSource = raw
	want, _ := ja3Of(raw)

	if *ja3 {
		// 只连本机：监听一个 TCP 端口，让 utls 发出 ClientHello 后读取原始字节
		ln, _ := net.Listen("tcp", "127.0.0.1:0")
		defer ln.Close()
		got := make(chan []byte, 1)
		go func() {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			buf := make([]byte, 16<<10)
			k, _ := c.Read(buf)
			got <- buf[:k]
			c.Close()
		}()
		tcp, _ := net.Dial("tcp", ln.Addr().String())
		fresh, _, _ := cloneSpec(spec)
		uc := utls.UClient(tcp, &utls.Config{ServerName: "img.nga.cn"}, utls.HelloCustom)
		if err := uc.ApplyPreset(fresh); err != nil {
			fmt.Println("ApplyPreset 失败：", err)
			os.Exit(1)
		}
		go func() { _ = uc.Handshake() }()
		select {
		case b := <-got:
			g, err := ja3Of(b)
			fmt.Println("Electron 原始 JA3：", ja3Hash(want), want)
			fmt.Println("utls 复刻后的 JA3：", ja3Hash(g), g, err)
			fmt.Println("一致：", g == want)
		case <-time.After(5 * time.Second):
			fmt.Println("超时：未收到 ClientHello")
			os.Exit(1)
		}
		return
	}

	var t target
	if *dry {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte{0xff, 0xd8, 0xff, 0xe0, 1, 2, 3})
		}))
		defer srv.Close()
		t = target{addr: strings.TrimPrefix(srv.URL, "https://"), host: "img.nga.cn", path: "/attachments/dry.jpg", insecure: true}
	} else {
		if *rawURL == "" {
			fmt.Fprintln(os.Stderr, "真实模式需要 -url")
			os.Exit(2)
		}
		u, err := url.Parse(*rawURL)
		if err != nil || u.Scheme != "https" {
			fmt.Fprintln(os.Stderr, "只支持 https 地址")
			os.Exit(2)
		}
		port := u.Port()
		if port == "" {
			port = "443"
		}
		t = target{addr: net.JoinHostPort(u.Hostname(), port), host: u.Hostname(), path: u.RequestURI()}
	}

	ctx := context.Background()
	first := true
	for _, v := range strings.Split(*variants, ",") {
		for i := 1; i <= *n; i++ {
			if !first && !*dry {
				time.Sleep(*interval)
			}
			first = false
			var resp *http.Response
			var body []byte
			var err error
			start := time.Now()
			switch v {
			case "v0":
				resp, body, err = goGET(ctx, t, false)
			case "v1":
				resp, body, err = goGET(ctx, t, true)
			case "v2":
				resp, body, err = utlsGET(ctx, spec, t, "close")
			case "v3":
				resp, body, err = utlsGET(ctx, spec, t, "keep-alive")
			default:
				fmt.Println("未知变体", v)
				continue
			}
			if err != nil {
				fmt.Printf("%s #%d  错误：%v  (%v)\n", v, i, err, time.Since(start).Round(time.Millisecond))
				continue
			}
			fmt.Printf("%s #%d  %s  (%v)\n", v, i, describe(resp, body), time.Since(start).Round(time.Millisecond))
		}
	}
}
