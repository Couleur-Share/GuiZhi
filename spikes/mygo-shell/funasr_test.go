package main

// FunASR 本地转写引擎行为测试：全程使用测试替身（testdata/fakefunasr）与内存里的 HTTP 路由，
// 不安装 Python、不下载、不联网、不占用真实的 8620 端口。

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	fakeFunasrOnce sync.Once
	fakeFunasrPath string
	fakeFunasrErr  error
)

func fakeFunasrBinary(t *testing.T) string {
	t.Helper()
	if !isWindows() {
		t.Skip("托管引擎只在 Windows 上提供")
	}
	fakeFunasrOnce.Do(func() {
		dir, err := os.MkdirTemp("", "fakefunasr")
		if err != nil {
			fakeFunasrErr = err
			return
		}
		fakeFunasrPath = filepath.Join(dir, "fakefunasr.exe")
		if out, err := exec.Command("go", "build", "-o", fakeFunasrPath, "./testdata/fakefunasr").CombinedOutput(); err != nil {
			fakeFunasrErr = fmt.Errorf("%v: %s", err, out)
		}
	})
	if fakeFunasrErr != nil {
		t.Skipf("无法编译测试替身：%v", fakeFunasrErr)
	}
	return fakeFunasrPath
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

type funasrFixture struct {
	t          *testing.T
	l          *Library
	e          *funasrEngine
	paths      funasrPaths
	mux        *http.ServeMux
	pypi       string // PyPI JSON 响应体
	pypiStatus int    // 非 0 时 PyPI 路由直接返回该状态码
	hitsMu     sync.Mutex
	hits       map[string]int
}

func newFunasrFixture(t *testing.T) *funasrFixture {
	t.Helper()
	fakeFunasrBinary(t)
	fx := &funasrFixture{t: t, mux: http.NewServeMux(), hits: map[string]int{}}
	fx.l = &Library{paths: Paths{UserData: t.TempDir()}, configDir: t.TempDir()}
	fx.l.toolsHTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		fx.hitsMu.Lock()
		fx.hits[r.URL.Host+r.URL.Path]++
		fx.hitsMu.Unlock()
		rec := httptest.NewRecorder()
		fx.mux.ServeHTTP(rec, r)
		resp := rec.Result()
		resp.Request = r
		return resp, nil
	})}
	e := fx.l.funasr()
	e.port = freePort(t)
	e.healthPoll = 30 * time.Millisecond
	e.stopSettle = 300 * time.Millisecond
	e.waitStep = 20 * time.Millisecond
	e.removeRetryDelay = 300 * time.Millisecond
	e.platform = "win32"
	e.pypiURL = "https://pypi.test/pypi/funasr/json"
	e.pythonURLs = []string{"https://dl.test/python.tar.gz"}
	e.checksumURLs = []string{"https://dl.test/SHA256SUMS"}
	e.lookup = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	}
	e.diskSpace = func(string) (int64, int64, error) { return 1 << 40, 4096, nil }
	fx.e, fx.paths = e, fx.l.funasrPaths()
	fx.mux.HandleFunc("/pypi/funasr/json", func(w http.ResponseWriter, r *http.Request) {
		if fx.pypiStatus != 0 {
			http.Error(w, "x", fx.pypiStatus)
			return
		}
		fmt.Fprint(w, fx.pypi)
	})
	t.Cleanup(func() { e.stop(); time.Sleep(100 * time.Millisecond) })
	return fx
}

func (fx *funasrFixture) hit(key string) int {
	fx.hitsMu.Lock()
	defer fx.hitsMu.Unlock()
	return fx.hits[key]
}

func pypiBody(version string, yanked bool, files ...bool) string {
	urls := []map[string]any{}
	for _, y := range files {
		urls = append(urls, map[string]any{"yanked": y})
	}
	b, _ := json.Marshal(map[string]any{"info": map[string]any{"version": version, "yanked": yanked}, "urls": urls})
	return string(b)
}

func (fx *funasrFixture) setPyPI(version string) { fx.pypi = pypiBody(version, false, false) }

// installLayout 摆出「已装好」的目录：venv 里的 python 是测试替身，funasr 包与 state.json 齐全。
func (fx *funasrFixture) installLayout(version string) {
	fx.t.Helper()
	p := fx.paths
	data, err := os.ReadFile(fakeFunasrBinary(fx.t))
	if err != nil {
		fx.t.Fatal(err)
	}
	for path, content := range map[string][]byte{p.VenvPython: data, p.FunasrPackage: nil, filepath.Join(p.VenvDir, "keep.txt"): []byte("sentinel")} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			fx.t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o755); err != nil {
			fx.t.Fatal(err)
		}
	}
	if err := writeFunasrState(funasrInstallState{FunasrVersion: version, PythonVersion: "Python 3.12.13", Flavor: funasrFlavorPython, InstalledAt: "2026-01-02T03:04:05.000Z"}, p); err != nil {
		fx.t.Fatal(err)
	}
}

func (fx *funasrFixture) serverStarts() int {
	b, err := os.ReadFile(filepath.Join(fx.paths.Root, "server-starts.log"))
	if err != nil {
		return 0
	}
	return len(strings.Split(strings.TrimSpace(string(b)), "\n"))
}

func (fx *funasrFixture) waitStopped() {
	fx.t.Helper()
	for i := 0; i < 100; i++ {
		if !fx.e.isPortListening(200 * time.Millisecond) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	fx.t.Fatal("服务进程没有退出")
}

func pythonTarGz(t *testing.T, withPython bool) []byte {
	t.Helper()
	bin, err := os.ReadFile(fakeFunasrBinary(t))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	add := func(name string, data []byte, mode int64) {
		tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(data)), Typeflag: tar.TypeReg})
		tw.Write(data)
	}
	tw.WriteHeader(&tar.Header{Name: "python/", Typeflag: tar.TypeDir, Mode: 0o755})
	if withPython {
		add("python/python.exe", bin, 0o755)
	}
	add("python/Lib/readme.txt", []byte("hello"), 0o644)
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func (fx *funasrFixture) serveInstallAssets(archive []byte, goodChecksum bool) {
	fx.mux.HandleFunc("/python.tar.gz", serveBytes(archive))
	sum := sha256.Sum256(archive)
	hexSum := hex.EncodeToString(sum[:])
	if !goodChecksum {
		hexSum = strings.Repeat("a", 64)
	}
	fx.mux.HandleFunc("/SHA256SUMS", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  %s\n", hexSum, pythonAssetName)
	})
}

// ── 外部命令 ──

func TestFunasrToolRunner(t *testing.T) {
	fake := fakeFunasrBinary(t)
	base := filepath.Base(fake)
	var last string
	out, err := runFunasrToolProcess(fake, []string{"--lines", "a", "b", "c"}, funasrToolOptions{OnOutput: func(l string) { last = l }})
	if err != nil || strings.ReplaceAll(out, "\r\n", "\n") != "a\nb\nc\n" || last != "c" {
		t.Errorf("正常输出：%q / %v / 最后一行 %q", out, err, last)
	}
	_, err = runFunasrToolProcess(fake, []string{"--exit", "7", "boom"}, funasrToolOptions{})
	if err == nil || err.Error() != base+" 退出码 7: boom" {
		t.Errorf("非零退出：%v", err)
	}
	_, err = runFunasrToolProcess(fake, []string{"--exit", "1", "Missing dependencies for SOCKS support"}, funasrToolOptions{})
	if err == nil || !strings.HasPrefix(err.Error(), funasrSocksProxyHint+"\n原始错误：") {
		t.Errorf("SOCKS 提示改写：%v", err)
	}
	_, err = runFunasrToolProcess(fake, []string{"--exit", "1", strings.Repeat("x", 400)}, funasrToolOptions{})
	if err == nil || !strings.HasSuffix(err.Error(), ": "+strings.Repeat("x", 300)) {
		t.Errorf("错误尾部应只留 300 个码元：%v", err)
	}
	start := time.Now()
	_, err = runFunasrToolProcess(fake, []string{"--sleep"}, funasrToolOptions{Timeout: 400 * time.Millisecond})
	if err == nil || err.Error() != base+" 执行超时" || time.Since(start) > 20*time.Second {
		t.Errorf("超时：%v（%v）", err, time.Since(start))
	}
	if _, err := runFunasrToolProcess(filepath.Join(t.TempDir(), "nope.exe"), nil, funasrToolOptions{}); err == nil {
		t.Error("不存在的可执行文件应报错")
	}
}

func TestFunasrLogSink(t *testing.T) {
	e := &funasrEngine{}
	sink := &funasrLogSink{e: e, gen: e.logGen}
	zh := []byte("中")
	sink.write(append([]byte("a"), zh[:1]...)) // 一个字符的字节被拆在两块里
	sink.write(append(append([]byte{}, zh[1:]...), 'b'))
	if e.logTail != "a中b" {
		t.Errorf("跨块 UTF-8 应拼回：%q", e.logTail)
	}
	sink.write([]byte("[guizhi-asr] tick\n"))
	if e.lastActivityAt.IsZero() {
		t.Error("心跳应更新最后活动时间")
	}
	e.mu.Lock()
	e.detachLoggingLocked()
	e.mu.Unlock()
	before := e.logTail
	sink.write([]byte("旧进程的输出"))
	if e.logTail != before {
		t.Error("摘掉之后旧进程不应再写日志")
	}
	fresh := &funasrLogSink{e: e, gen: e.logGen}
	fresh.write([]byte(strings.Repeat("x", 6000)))
	if utf16Len(e.logTail) != funasrLogTailMax {
		t.Errorf("日志尾部应裁到 %d：%d", funasrLogTailMax, utf16Len(e.logTail))
	}
}

func TestFunasrRunExclusiveFIFO(t *testing.T) {
	e := &funasrEngine{}
	var mu sync.Mutex
	var order []int
	active, maxActive := 0, 0
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := runExclusiveLocalTranscription(e, func() (int, error) {
				mu.Lock()
				active++
				maxActive = max(maxActive, active)
				order = append(order, i)
				mu.Unlock()
				time.Sleep(30 * time.Millisecond)
				mu.Lock()
				active--
				mu.Unlock()
				if i == 0 {
					return 0, fmt.Errorf("第一条失败")
				}
				return i, nil
			})
			if (i == 0) != (err != nil) {
				t.Errorf("#%d 的错误只应属于它自己：%v", i, err)
			}
		}()
		time.Sleep(15 * time.Millisecond) // 保证入队顺序
	}
	wg.Wait()
	if maxActive != 1 || fmt.Sprint(order) != "[0 1 2 3 4]" {
		t.Errorf("应严格串行且先进先出：max=%d order=%v", maxActive, order)
	}
	if e.pendingTranscriptions != 0 {
		t.Errorf("排队计数应归零：%d", e.pendingTranscriptions)
	}
	e.maintenanceInFlight = true
	if _, err := runExclusiveLocalTranscription(e, func() (int, error) { return 1, nil }); err == nil || err.Error() != "本地转写引擎正在更新，请稍后重试" {
		t.Errorf("维护中应拒绝转写：%v", err)
	}
}

func TestExtractTarGzSafety(t *testing.T) {
	build := func(entries func(tw *tar.Writer)) string {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		entries(tw)
		tw.Close()
		gz.Close()
		p := filepath.Join(t.TempDir(), "a.tar.gz")
		os.WriteFile(p, buf.Bytes(), 0o600)
		return p
	}
	reg := func(tw *tar.Writer, name, body string) {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg})
		tw.Write([]byte(body))
	}
	for _, bad := range []string{"../evil.txt", "a/../../evil.txt", "/abs.txt", "C:/x.txt", "..\\evil.txt"} {
		dest := t.TempDir()
		err := extractTarGz(context.Background(), build(func(tw *tar.Writer) { reg(tw, bad, "x") }), dest)
		if err == nil || !strings.Contains(err.Error(), "越界") {
			t.Errorf("%q 应被拒绝：%v", bad, err)
		}
		if es, _ := os.ReadDir(filepath.Dir(dest)); len(es) > 1 {
			// 同级目录里不应出现被写出去的文件
			for _, e := range es {
				if e.Name() == "evil.txt" {
					t.Errorf("%q 越界写出了文件", bad)
				}
			}
		}
	}
	dest := t.TempDir()
	err := extractTarGz(context.Background(), build(func(tw *tar.Writer) {
		tw.WriteHeader(&tar.Header{Name: "p/", Typeflag: tar.TypeDir, Mode: 0o755})
		reg(tw, "p/a.txt", "hello")
		tw.WriteHeader(&tar.Header{Name: "p/link.txt", Typeflag: tar.TypeLink, Linkname: "p/a.txt"})
		tw.WriteHeader(&tar.Header{Name: "p/sym", Typeflag: tar.TypeSymlink, Linkname: "../../etc"})
		reg(tw, "p/deep/er/b.txt", "deep")
	}), dest)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"p/a.txt": "hello", "p/link.txt": "hello", "p/deep/er/b.txt": "deep"} {
		if b, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(name))); err != nil || string(b) != want {
			t.Errorf("%s：%q %v", name, b, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(dest, "p", "sym")); err == nil {
		t.Error("符号链接应被跳过")
	}
	if err := extractTarGz(context.Background(), build(func(tw *tar.Writer) {
		tw.WriteHeader(&tar.Header{Name: "p/l", Typeflag: tar.TypeLink, Linkname: "p/missing"})
	}), t.TempDir()); err == nil || !strings.Contains(err.Error(), "硬链接目标缺失") {
		t.Errorf("硬链接目标缺失：%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := extractTarGz(ctx, build(func(tw *tar.Writer) { reg(tw, "a", "x") }), t.TempDir()); err == nil {
		t.Error("已取消的上下文应中止解包")
	}
}
