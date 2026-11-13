package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var (
	fakeToolOnce sync.Once
	fakeToolPath string
	fakeToolErr  error
)

// fakeToolBinary 编译一次测试替身（ffmpeg / yt-dlp 的 --version 输出），返回其路径。
func fakeToolBinary(t *testing.T) string {
	t.Helper()
	fakeToolOnce.Do(func() {
		dir, err := os.MkdirTemp("", "fakeprobe")
		if err != nil {
			fakeToolErr = err
			return
		}
		name := "fakeprobe"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		fakeToolPath = filepath.Join(dir, name)
		out, err := exec.Command("go", "build", "-o", fakeToolPath, "./testdata/fakeprobe").CombinedOutput()
		if err != nil {
			fakeToolErr = fmt.Errorf("%v: %s", err, out)
		}
	})
	if fakeToolErr != nil {
		t.Skipf("无法编译测试替身：%v", fakeToolErr)
	}
	return fakeToolPath
}

func copyToolFile(t *testing.T, src, dst string) {
	t.Helper()
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, b, 0o755); err != nil {
		t.Fatal(err)
	}
}

func newToolsLibrary(t *testing.T) *Library {
	t.Helper()
	_, l := loadGolden(t)
	l.paths = Paths{UserData: t.TempDir()}
	return l
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestToolParsers(t *testing.T) {
	sums := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  yt-dlp\r\n" +
		"BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB *yt-dlp.exe\n" + "short  x\n"
	if got := parseSha256Sums(sums, "yt-dlp.exe"); got != strings.Repeat("b", 64) {
		t.Errorf("二进制模式行: %q", got)
	}
	if got := parseSha256Sums(sums, "yt-dlp"); got != strings.Repeat("a", 64) {
		t.Errorf("普通行: %q", got)
	}
	if parseSha256Sums(sums, "missing") != "" || parseSha256Sums(sums, "x") != "" {
		t.Errorf("缺失或哈希长度不对应返回空")
	}
	if parseReleaseTag("https://github.com/yt-dlp/yt-dlp/releases/tag/2026.07.15?x=1") != "2026.07.15" ||
		parseReleaseTag("/releases/tag/a%20b") != "a b" || parseReleaseTag("https://x/releases/latest") != "" {
		t.Errorf("parseReleaseTag")
	}
	for in, want := range map[string]string{"N-125753-g6095372a70-20260724": "20260724", "8.1-essentials_build": "", "20260724": ""} {
		got := parseFfmpegBuildDate(&in)
		if (want == "") != (got == nil) || (got != nil && *got != want) {
			t.Errorf("parseFfmpegBuildDate(%q) = %v", in, got)
		}
	}
	if parseFfmpegBuildDate(nil) != nil {
		t.Errorf("nil 版本")
	}
	if d := toBuildDate("Fri, 24 Jul 2026 23:59:59 GMT"); d == nil || *d != "20260724" {
		t.Errorf("toBuildDate: %v", d)
	}
	if toBuildDate("") != nil || toBuildDate("not a date") != nil {
		t.Errorf("无效 Last-Modified 应为 nil")
	}
	// 与 Node 的 localeCompare(…, {numeric:true}) 逐对一致（基准由 node 实测得出）
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"2026.07.15", "2026.07.04", 1}, {"2026.07.04", "2026.07.15", -1}, {"2026.07.15", "2026.07.15", 0},
		{"2026.10.01", "2026.9.30", 1}, {"2026.07.15.232000", "2026.07.15", 1}, {"2026.07.15", "2026.07.15.232000", -1},
		{"2026.07.15.9", "2026.07.15.10", -1}, {"2026.07.15-a", "2026.07.15-b", -1}, {"10", "9", 1}, {"a10", "a9", 1},
		{"v2", "v10", -1}, {"1.0", "1.00", 0}, {"01", "1", 0}, {"2026.07.15", "nightly", -1},
	} {
		if got := numericLocaleCompare(c.a, c.b); got != c.want {
			t.Errorf("numericLocaleCompare(%q,%q) = %d，期望 %d", c.a, c.b, got, c.want)
		}
	}
	cur, newer, same := "2026.07.04", "2026.07.15", "2026.07.04"
	if c := checkYtDlpUpdate(&cur, newer); !c.UpdateAvailable || *c.Latest != newer {
		t.Errorf("应有更新: %+v", c)
	}
	if c := checkYtDlpUpdate(&cur, same); c.UpdateAvailable {
		t.Errorf("同版本不应更新: %+v", c)
	}
	if c := checkYtDlpUpdate(&cur, ""); c.UpdateAvailable || c.Latest != nil {
		t.Errorf("查询失败 latest 应为 nil: %+v", c)
	}
	if c := checkYtDlpUpdate(nil, newer); c.UpdateAvailable {
		t.Errorf("非内置版没有版本比较: %+v", c)
	}
	ffCur, ffNew := "N-1-g1-20260701", "20260724"
	if c := checkFfmpegUpdate(&ffCur, &ffNew); !c.UpdateAvailable || *c.Current != "20260701" {
		t.Errorf("ffmpeg 应有更新: %+v", c)
	}
	if c := checkFfmpegUpdate(&ffCur, nil); c.UpdateAvailable {
		t.Errorf("ffmpeg 查询失败")
	}
}

func TestStatusCacheTTLAndMerge(t *testing.T) {
	c := newStatusCache[int]()
	now := time.Unix(0, 0)
	c.now = func() time.Time { return now }
	var calls atomic.Int32
	release := make(chan struct{})
	load := func() (int, error) { n := calls.Add(1); <-release; return int(n), nil }
	var wg sync.WaitGroup
	results := make([]int, 3)
	for i := range results {
		wg.Add(1)
		go func() { defer wg.Done(); results[i], _ = c.read("k", load, false) }()
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	if calls.Load() != 1 || results[0] != 1 || results[1] != 1 || results[2] != 1 {
		t.Fatalf("并发探测应合并: calls=%d %v", calls.Load(), results)
	}
	fast := func() (int, error) { return int(calls.Add(1)), nil }
	if v, _ := c.read("k", fast, false); v != 1 {
		t.Errorf("TTL 内应命中缓存: %d", v)
	}
	if v, _ := c.read("other", fast, false); v != 2 {
		t.Errorf("键变化应未命中: %d", v)
	}
	now = now.Add(engineStatusTTL + time.Second)
	if v, _ := c.read("other", fast, false); v != 3 {
		t.Errorf("过期应重新探测: %d", v)
	}
	if v, _ := c.read("other", fast, true); v != 4 {
		t.Errorf("force 应绕过缓存: %d", v)
	}
	c.invalidate()
	if v, _ := c.read("other", fast, false); v != 5 {
		t.Errorf("invalidate 后应重新探测: %d", v)
	}
}

func TestEngineVersionCache(t *testing.T) {
	l := newToolsLibrary(t)
	fake := fakeToolBinary(t)
	exe := filepath.Join(t.TempDir(), "tool.exe")
	copyToolFile(t, fake, exe)
	var spawns int
	probe := func(ctx context.Context, e string) (string, bool) { spawns++; return probeYtDlpVersion(ctx, e) }
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if v, ok := l.withCachedVersion(probe, false)(ctx, exe); !ok || v != "2026.07.15" {
			t.Fatalf("探测: %q %v", v, ok)
		}
	}
	if spawns != 1 {
		t.Errorf("同一文件只应 spawn 一次: %d", spawns)
	}
	l.withCachedVersion(probe, true)(ctx, exe)
	if spawns != 2 {
		t.Errorf("force 应重新探测: %d", spawns)
	}
	// 文件变化（大小/mtime）后缓存失效；失败结果不缓存
	f, _ := os.OpenFile(exe, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("x")
	f.Close()
	l.withCachedVersion(probe, false)(ctx, exe)
	if spawns != 3 {
		t.Errorf("文件变化后应重新探测: %d", spawns)
	}
	t.Setenv("FAKE_TOOL_FAIL", "1")
	broken := filepath.Join(t.TempDir(), "broken.exe")
	copyToolFile(t, fake, broken)
	for i := 0; i < 2; i++ {
		if _, ok := l.withCachedVersion(probe, false)(ctx, broken); ok {
			t.Fatal("非零退出应判不可用")
		}
	}
	if spawns != 5 {
		t.Errorf("失败不缓存，应每次探测: %d", spawns)
	}
	if _, ok := l.withCachedVersion(probe, false)(ctx, filepath.Join(t.TempDir(), "missing.exe")); ok {
		t.Error("不存在的文件应判不可用")
	}
}

func TestToolStatusResolutionOrder(t *testing.T) {
	l := newToolsLibrary(t)
	fake := fakeToolBinary(t)
	ctx := context.Background()
	p := nodePlatform()
	t.Setenv("PATH", t.TempDir()) // 先清空 PATH，避免命中本机真实工具

	none := l.ytDlpStatus(ctx, "", probeYtDlpVersion, p)
	if none.Installed || none.Source != nil || none.ManagedPath != l.managedYtDlpPath(p) || !none.InstallSupported {
		t.Errorf("都没有: %+v", none)
	}
	// PATH
	pathDir := t.TempDir()
	copyToolFile(t, fake, filepath.Join(pathDir, managedYtDlpBinaryName(p)))
	t.Setenv("PATH", pathDir)
	if s := l.ytDlpStatus(ctx, "", probeYtDlpVersion, p); !s.Installed || *s.Source != "path" || s.Path != "yt-dlp" || s.Version != "2026.07.15" {
		t.Errorf("PATH: %+v", s)
	}
	// 托管版优先于 PATH
	copyToolFile(t, fake, l.managedYtDlpPath(p))
	if s := l.ytDlpStatus(ctx, "", probeYtDlpVersion, p); *s.Source != "managed" || s.Path != l.managedYtDlpPath(p) {
		t.Errorf("托管: %+v", s)
	}
	// 自定义路径优先；坏的自定义路径不回退
	custom := filepath.Join(t.TempDir(), "mine.exe")
	copyToolFile(t, fake, custom)
	if s := l.ytDlpStatus(ctx, "  "+custom+" ", probeYtDlpVersion, p); *s.Source != "custom" || s.Path != custom {
		t.Errorf("自定义: %+v", s)
	}
	if s := l.ytDlpStatus(ctx, filepath.Join(t.TempDir(), "nope.exe"), probeYtDlpVersion, p); s.Installed || s.Source != nil || s.Version != "" || s.Path == "" {
		t.Errorf("坏的自定义路径: %+v", s)
	}
	// 托管副本损坏 → 回落 PATH
	if err := os.WriteFile(l.managedYtDlpPath(p), []byte("not an exe"), 0o755); err != nil {
		t.Fatal(err)
	}
	if s := l.ytDlpStatus(ctx, "", probeYtDlpVersion, p); *s.Source != "path" {
		t.Errorf("托管损坏应回落 PATH: %+v", s)
	}

	// ffmpeg：安装能力与提示命令按平台
	if s := l.ffmpegStatus(ctx, "", probeFfmpegVersion, "darwin"); s.InstallSupported || s.InstallHintCommand != "brew install ffmpeg" {
		t.Errorf("darwin: %+v", s)
	}
	if s := l.ffmpegStatus(ctx, "", probeFfmpegVersion, "linux"); s.InstallSupported || s.InstallHintCommand != "" {
		t.Errorf("linux: %+v", s)
	}
	copyToolFile(t, fake, filepath.Join(pathDir, managedFfmpegBinaryName(p)))
	if s := l.ffmpegStatus(ctx, "", probeFfmpegVersion, "win32"); !s.Installed || *s.Source != "path" || s.Version != "N-125753-g6095372a70-20260724" || !s.InstallSupported {
		t.Errorf("ffmpeg PATH: %+v", s)
	}
}

type toolServer struct {
	*httptest.Server
	hits sync.Map
}

func newToolServer(t *testing.T, routes map[string]http.HandlerFunc) *toolServer {
	ts := &toolServer{}
	ts.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, _ := ts.hits.LoadOrStore(r.URL.Path, new(atomic.Int32))
		n.(*atomic.Int32).Add(1)
		if h, ok := routes[r.URL.Path]; ok {
			h(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func serveBytes(b []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(b)))
		w.Write(b)
	}
}

func TestInstallYtDlpMirrorsAndChecksum(t *testing.T) {
	l := newToolsLibrary(t)
	p := nodePlatform()
	good, err := os.ReadFile(fakeToolBinary(t))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(good)
	asset := ytDlpAssetName(p)
	srv := newToolServer(t, map[string]http.HandlerFunc{
		"/bad":      func(w http.ResponseWriter, r *http.Request) { http.Error(w, "boom", 500) },
		"/tampered": serveBytes(append(append([]byte{}, good...), 'x')),
		"/good":     serveBytes(good),
		"/not-exe":  serveBytes([]byte("hello")),
		"/SHA2-256S": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), asset)
		},
	})
	ctx := context.Background()
	var progress []toolDownloadProgress
	onProgress := func(pg toolDownloadProgress) { progress = append(progress, pg) }

	// 第一源失败、第二源被篡改（校验和不匹配）、第三源成功
	version, err := l.installYtDlp(ctx, p, []string{srv.URL + "/bad", srv.URL + "/tampered", srv.URL + "/good"}, []string{srv.URL + "/missing", srv.URL + "/SHA2-256S"}, onProgress)
	if err != nil || version != "2026.07.15" {
		t.Fatalf("安装: %q %v", version, err)
	}
	if !fileExists(l.managedYtDlpPath(p)) || fileExists(filepath.Join(filepath.Dir(l.managedYtDlpPath(p)), "yt-dlp.partial")) {
		t.Errorf("托管副本应就位且无残留临时文件")
	}
	if len(progress) == 0 || progress[len(progress)-1].Transferred != int64(len(good)) || progress[len(progress)-1].Total == nil {
		t.Errorf("进度回调不符: %v", progress)
	}
	if n, _ := srv.hits.Load("/tampered"); n == nil || n.(*atomic.Int32).Load() != 1 {
		t.Errorf("被篡改的源应只尝试一次")
	}

	// 全部失败：汇总各源原因，且旧托管副本保持不动
	before, _ := os.ReadFile(l.managedYtDlpPath(p))
	_, err = l.installYtDlp(ctx, p, []string{srv.URL + "/bad", srv.URL + "/tampered", srv.URL + "/not-exe"}, []string{srv.URL + "/SHA2-256S"}, nil)
	if err == nil || !strings.HasPrefix(err.Error(), "所有下载源均失败——") || !strings.Contains(err.Error(), "HTTP 500") || !strings.Contains(err.Error(), "校验和不匹配") {
		t.Fatalf("应汇总失败原因: %v", err)
	}
	if after, _ := os.ReadFile(l.managedYtDlpPath(p)); !bytes.Equal(before, after) {
		t.Errorf("失败不应改动已安装的托管副本")
	}

	// 取不到校验和时跳过校验（降级），但文件必须能跑
	if _, err := l.installYtDlp(ctx, p, []string{srv.URL + "/not-exe"}, []string{srv.URL + "/missing"}, nil); err == nil || !strings.Contains(err.Error(), "下载的文件无法运行") {
		t.Errorf("不可执行文件应被拒绝: %v", err)
	}
	if v, err := l.installYtDlp(ctx, p, []string{srv.URL + "/good"}, []string{srv.URL + "/missing"}, nil); err != nil || v != "2026.07.15" {
		t.Errorf("无校验和降级安装: %q %v", v, err)
	}

	// 同时只允许一个安装任务
	slow := make(chan struct{})
	slowSrv := newToolServer(t, map[string]http.HandlerFunc{"/slow": func(w http.ResponseWriter, r *http.Request) { <-slow; w.Write(good) }})
	done := make(chan error, 1)
	go func() { _, err := l.installYtDlp(ctx, p, []string{slowSrv.URL + "/slow"}, nil, nil); done <- err }()
	time.Sleep(150 * time.Millisecond)
	if _, err := l.installYtDlp(ctx, p, []string{srv.URL + "/good"}, nil, nil); err == nil || err.Error() != "已有安装任务进行中" {
		t.Errorf("并发安装应被拒绝: %v", err)
	}
	close(slow)
	if err := <-done; err != nil {
		t.Errorf("慢速安装最终应成功: %v", err)
	}
}

func TestInstallFfmpegFromZip(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("托管 ffmpeg 仅 Windows 提供")
	}
	l := newToolsLibrary(t)
	good, err := os.ReadFile(fakeToolBinary(t))
	if err != nil {
		t.Fatal(err)
	}
	makeZip := func(entries map[string][]byte) []byte {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		for name, data := range entries {
			w, _ := zw.Create(name)
			w.Write(data)
		}
		zw.Close()
		return buf.Bytes()
	}
	srv := newToolServer(t, map[string]http.HandlerFunc{
		"/ok":        serveBytes(makeZip(map[string][]byte{"ffmpeg-master-latest-win64-gpl/bin/ffmpeg.exe": good, "ffmpeg-master-latest-win64-gpl/bin/ffprobe.exe": []byte("x")})),
		"/no-bin":    serveBytes(makeZip(map[string][]byte{"readme.txt": []byte("hi")})),
		"/not-run":   serveBytes(makeZip(map[string][]byte{"a/bin/ffmpeg.exe": []byte("MZ nope")})),
		"/not-zip":   serveBytes([]byte("plain")),
		"/backslash": serveBytes(makeZip(map[string][]byte{"a\\bin\\ffmpeg.exe": good})),
	})
	ctx := context.Background()
	version, err := l.installFfmpeg(ctx, "win32", []string{srv.URL + "/no-bin", srv.URL + "/not-zip", srv.URL + "/not-run", srv.URL + "/ok"}, nil)
	if err != nil || version != "N-125753-g6095372a70-20260724" {
		t.Fatalf("安装: %q %v", version, err)
	}
	dir := l.paths.ToolsDir()
	for _, leftover := range []string{"ffmpeg.download.zip", "ffmpeg.partial.exe"} {
		if fileExists(filepath.Join(dir, leftover)) {
			t.Errorf("残留临时文件 %s", leftover)
		}
	}
	if _, err := l.installFfmpeg(ctx, "win32", []string{srv.URL + "/no-bin", srv.URL + "/not-zip", srv.URL + "/not-run"}, nil); err == nil ||
		!strings.Contains(err.Error(), "压缩包中未找到 bin/ffmpeg.exe") || !strings.Contains(err.Error(), "下载的文件无法运行") {
		t.Errorf("汇总失败原因: %v", err)
	}
	if _, err := l.installFfmpeg(ctx, "win32", []string{srv.URL + "/backslash"}, nil); err != nil {
		t.Errorf("反斜杠路径的 zip 条目应识别: %v", err)
	}
	if _, err := l.installFfmpeg(ctx, "linux", []string{srv.URL + "/ok"}, nil); err == nil || err.Error() != "当前平台请使用系统包管理器安装 ffmpeg" {
		t.Errorf("非 Windows 应拒绝: %v", err)
	}
	if ok, err := l.removeManagedFfmpeg(); !ok || err != nil {
		t.Errorf("移除: %v %v", ok, err)
	}
	if ok, _ := l.removeManagedFfmpeg(); ok {
		t.Errorf("再次移除应返回 false")
	}
}

// 经 IPC 层：状态缓存、更新检查（HTTP 全部走注入的传输层，不访问真实网络）、安装推送进度、选择器登记。
func TestManagedToolsIPC(t *testing.T) {
	l := newToolsLibrary(t)
	fake := fakeToolBinary(t)
	t.Setenv("PATH", t.TempDir())
	l.toolsHTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		h := http.Header{}
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest") && r.Method == http.MethodGet:
			h.Set("Location", "https://github.com/yt-dlp/yt-dlp/releases/tag/2026.08.01")
			return &http.Response{StatusCode: 302, Header: h, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
		case r.Method == http.MethodHead && strings.Contains(r.URL.Path, "ffmpeg-master-latest-win64-gpl.zip"):
			h.Set("Last-Modified", "Sat, 25 Jul 2026 01:00:00 GMT")
			return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
		}
		return &http.Response{StatusCode: 404, Header: h, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}
	b := &Bridge{handlers: map[string]handler{}}
	l.registerManagedTools(b)
	call := func(ch string, args ...any) any {
		t.Helper()
		raw := make([]json.RawMessage, len(args))
		for i, a := range args {
			raw[i], _ = json.Marshal(a)
		}
		out, err := b.handlers[ch](context.Background(), raw)
		if err != nil {
			t.Fatalf("%s: %v", ch, err)
		}
		return toGeneric(t, out)
	}
	p := nodePlatform()

	st := call("ytdlp:status").(map[string]any)
	if st["installed"] != false || st["source"] != nil || st["installSupported"] != true {
		t.Fatalf("初始状态: %v", st)
	}
	// 状态被缓存：装好之后不 force 仍是旧值，force 才刷新
	copyToolFile(t, fake, l.managedYtDlpPath(p))
	if st = call("ytdlp:status").(map[string]any); st["installed"] != false {
		t.Errorf("缓存期内应沿用旧状态: %v", st)
	}
	if st = call("ytdlp:status", true).(map[string]any); st["installed"] != true || st["source"] != "managed" || st["version"] != "2026.07.15" {
		t.Errorf("force 应刷新: %v", st)
	}
	// 更新检查：当前内置版 2026.07.15 < 远端 tag 2026.08.01
	if up := call("ytdlp:checkUpdate").(map[string]any); up["current"] != "2026.07.15" || up["latest"] != "2026.08.01" || up["updateAvailable"] != true {
		t.Errorf("yt-dlp 更新检查: %v", up)
	}
	if up := call("ffmpeg:checkUpdate").(map[string]any); up["current"] != nil || up["latest"] != "20260725" || up["updateAvailable"] != false {
		t.Errorf("ffmpeg 非内置版没有版本比较: %v", up)
	}
	// 自定义路径：settings:set 只接受登记过的路径
	custom := filepath.Join(t.TempDir(), "mine.exe")
	copyToolFile(t, fake, custom)
	if isAcceptableBinaryPath(mustRaw(custom)) {
		t.Errorf("未登记的路径不应放行")
	}
	l.pickExecutable = func(title string) (string, error) {
		if title != "选择 yt-dlp 可执行文件" && title != "选择 ffmpeg 可执行文件" {
			t.Errorf("对话框标题: %q", title)
		}
		return custom, nil
	}
	if got := call("ytdlp:pickBinary"); got != custom {
		t.Errorf("pickBinary: %v", got)
	}
	if !isAcceptableBinaryPath(mustRaw(custom)) || !isAcceptableBinaryPath(mustRaw("  ")) || isAcceptableBinaryPath(mustRaw(custom+"x")) || isAcceptableBinaryPath(json.RawMessage("5")) {
		t.Errorf("登记表语义不符")
	}
	l.pickExecutable = func(string) (string, error) { return "", nil }
	if got := call("ffmpeg:pickBinary"); got != nil {
		t.Errorf("取消选择应返回 null: %v", got)
	}
	// 移除
	if got := call("ytdlp:remove"); got != true {
		t.Errorf("移除: %v", got)
	}
	if got := call("ytdlp:remove"); got != false {
		t.Errorf("再次移除: %v", got)
	}
	// 安装失败走 {success:false,error}：toolsHTTP 对下载一律 404
	res := call("ytdlp:install").(map[string]any)
	if res["success"] != false || !strings.HasPrefix(fmt.Sprint(res["error"]), "所有下载源均失败——") {
		t.Errorf("安装失败: %v", res)
	}
	if st = call("ytdlp:status").(map[string]any); st["installed"] != false {
		t.Errorf("安装后缓存应失效并重新探测: %v", st)
	}
}

func mustRaw(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}
