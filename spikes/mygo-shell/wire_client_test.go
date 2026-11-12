package main

// 线上对拍的离线前置：Go 版抓取层发出的真实线上字节（请求头顺序、Connection、TLS ClientHello）。
// 默认跳过；由 verify/wire/wire-compare.mts 起本地捕获服务器后设置端口环境变量再运行。
// 这里走产品的 NGA 请求路径（ngaFetch / ngaDefaultDownloadImage），请求只发往 127.0.0.1，不碰真实站点。

import (
	"context"
	"net"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"
)

func TestWireClient(t *testing.T) {
	plain, tlsPort := os.Getenv("GUIZHI_WIRE_PLAIN"), os.Getenv("GUIZHI_WIRE_TLS")
	if plain == "" || tlsPort == "" {
		t.Skip("未设置 GUIZHI_WIRE_PLAIN / GUIZHI_WIRE_TLS（由 verify/wire/wire-compare.mts 启动）")
	}
	target := func(port string) func(ctx context.Context, network, addr string) (net.Conn, error) {
		return func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, "127.0.0.1:"+port)
		}
	}
	mk := func(port string) *safeFetcher {
		return &safeFetcher{
			proxy: newNetProxyState(),
			lookup: func(context.Context, string) ([]netip.Addr, error) {
				return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
			},
			dialOverride: target(port),
			idle:         3 * time.Second,
		}
	}
	const topic = "37194262"
	ctx := context.Background()

	// 明文：页面请求（带 guestJs Cookie、接受 403）与图片下载（无 Cookie），与 TS 版 ts-client.ts 一一对应
	l := &Library{webFetcher: mk(plain)}
	get := l.ngaFetch(ngaDeps{})
	page := strings.Replace(ngaPageURL(topic, 1, nil), "https://bbs.nga.cn", "http://bbs.nga.cn:"+plain, 1)
	if _, err := get(ctx, page, ngaRequestOptions(topic, "guestJs=abc", true)); err != nil {
		t.Fatalf("明文页面请求失败：%v", err)
	}
	dir, _, err := l.ngaDefaultDownloadImage(ctx, "http://img.nga.cn:"+plain+"/attachments/x.jpg")
	if err != nil {
		t.Fatalf("明文图片请求失败：%v", err)
	}
	_ = os.RemoveAll(dir)

	// TLS：只要把 ClientHello 发出去即可，握手必然失败（服务器读完就断开）
	lt := &Library{webFetcher: mk(tlsPort)}
	_, _ = lt.ngaFetch(ngaDeps{})(ctx, strings.Replace(page, "http://bbs.nga.cn:"+plain, "https://bbs.nga.cn:"+tlsPort, 1), ngaRequestOptions(topic, "", true))
}
