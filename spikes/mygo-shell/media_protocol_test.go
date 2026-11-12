package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 对应 local-media-protocol.ts 的 resolveLocalMediaProtocolPath：
// 段不能是 . / ..、不能含控制字符与冒号，扩展名走白名单，结果必须落在基准目录内。
func TestResolveLocalMediaPath(t *testing.T) {
	base := t.TempDir()
	cases := []struct {
		name string
		path string
		exts map[string]bool
		ok   bool
	}{
		{"普通文件", "a.png", localImageExts, true},
		{"大写扩展名", "A.PNG", localImageExts, true},
		{"子目录", "sub/a.jpg", localImageExts, true},
		{"反斜杠子目录", `sub\a.jpg`, localImageExts, true},
		{"空路径", "", localImageExts, false},
		{"点点上跳", "../a.png", localImageExts, false},
		{"中间点点", "sub/../../a.png", localImageExts, false},
		{"单点段", "./a.png", localImageExts, false},
		{"绝对路径", "/a.png", localImageExts, false},
		{"反斜杠绝对路径", `\a.png`, localImageExts, false},
		{"盘符冒号", "C:/a.png", localImageExts, false},
		{"控制字符", "a\x01.png", localImageExts, false},
		{"不在白名单的扩展名", "a.svg", localImageExts, false},
		{"没有扩展名", "a", localImageExts, false},
		{"视频扩展名不能走图片协议", "a.mp4", localImageExts, false},
		{"视频协议接受 mp4", "v.mp4", localVideoExts, true},
		{"视频协议拒绝图片", "a.png", localVideoExts, false},
	}
	for _, c := range cases {
		got, ok := resolveLocalMediaPath(base, c.path, c.exts)
		if ok != c.ok {
			t.Errorf("%s：path=%q 期望 ok=%v，得到 ok=%v（%s）", c.name, c.path, c.ok, ok, got)
			continue
		}
		if ok && !strings.HasPrefix(got, base+string(filepath.Separator)) {
			t.Errorf("%s：解析结果 %q 不在基准目录内", c.name, got)
		}
	}
}

// 处理器：命中返回文件内容与正确类型，缺失 404，非法路径 403，视频支持 Range。
func TestLocalMediaHandler(t *testing.T) {
	dir := t.TempDir()
	png := []byte("\x89PNG\r\n\x1a\n-fake-body-")
	if err := os.WriteFile(filepath.Join(dir, "a b.png"), png, 0o600); err != nil {
		t.Fatal(err)
	}
	video := []byte("0123456789abcdefghij")
	if err := os.WriteFile(filepath.Join(dir, "v.mp4"), video, 0o600); err != nil {
		t.Fatal(err)
	}
	// 目录外的同名诱饵文件，越权读取必须读不到
	outside := filepath.Join(filepath.Dir(dir), "secret.png")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(outside) })

	img := localMediaHandler(dir, localImageExts)
	vid := localMediaHandler(dir, localVideoExts)
	get := func(h http.Handler, target string, hdr map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", target, nil)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	rec := get(img, "local-image://localhost/a%20b.png", nil)
	if rec.Code != 200 || rec.Body.String() != string(png) || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("命中：code=%d type=%q body=%q", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
	if rec = get(img, "local-image://localhost/missing.png", nil); rec.Code != 404 {
		t.Fatalf("缺失文件应 404，得到 %d", rec.Code)
	}
	for _, bad := range []string{"local-image://localhost/..%2Fsecret.png", "local-image://localhost/sub/..%2F..%2Fsecret.png", "local-image://localhost/a.svg", "local-image://localhost/"} {
		if rec = get(img, bad, nil); rec.Code != 403 && rec.Code != 404 {
			t.Fatalf("%s 应拒绝，得到 %d", bad, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "secret") {
			t.Fatalf("%s 读到了目录外的文件", bad)
		}
	}
	if rec = get(img, "local-image://localhost/..%2Fsecret.png", nil); rec.Code != 403 {
		t.Fatalf("越权路径应 403，得到 %d", rec.Code)
	}
	if rec = get(img, "local-image://localhost/a%20b.png", map[string]string{"Origin": "http://127.0.0.1:1"}); rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("应带 nosniff")
	}

	rec = get(vid, "local-video://localhost/v.mp4", map[string]string{"Range": "bytes=2-5"})
	if rec.Code != 206 || rec.Body.String() != "2345" || rec.Header().Get("Accept-Ranges") != "bytes" {
		t.Fatalf("视频 Range：code=%d body=%q ar=%q", rec.Code, rec.Body.String(), rec.Header().Get("Accept-Ranges"))
	}
	if rec = get(vid, "local-video://localhost/v.mp4", nil); rec.Code != 200 || rec.Body.Len() != len(video) {
		t.Fatalf("视频整取：code=%d len=%d", rec.Code, rec.Body.Len())
	}
	if rec = get(img, "local-image://localhost/", nil); rec.Code == 200 {
		t.Fatalf("空路径不能成功")
	}
}
