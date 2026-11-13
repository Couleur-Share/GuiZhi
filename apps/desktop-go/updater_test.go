package main

// 更新器核心测试：全部使用本地 httptest 模拟 GitHub / 镜像，不访问真实网络。

import (
	"context"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type updaterFixture struct {
	srv         *httptest.Server
	u           *Updater
	mu          sync.Mutex
	statuses    []map[string]any
	installer   []byte
	exeHits     atomic.Int32
	latestTag   string
	atomTags    []string
	manifest    func(tag string) string
	spawned     [][]string
	opened      []string
	revealed    []string
	quit        chan struct{}
	apiReleases string
	ymlOK       func(tag, file string) bool // 非 nil 时决定某个 tag 下哪些 .yml 存在（灰度通道对拍用）
	dir         string
}

func sha512b64(b []byte) string {
	s := sha512.Sum512(b)
	return base64.StdEncoding.EncodeToString(s[:])
}

func newUpdaterFixture(t *testing.T, current string) *updaterFixture {
	t.Helper()
	f := &updaterFixture{quit: make(chan struct{}, 1), latestTag: "v0.30.0", atomTags: []string{"v0.30.0", "v0.29.0"}}
	f.installer = []byte(strings.Repeat("GUIZHI-INSTALLER-", 60000)) // ~1MB
	f.dir = t.TempDir()
	repo := "/" + updaterOwner + "/" + updaterRepo + "/releases"
	mux := http.NewServeMux()
	mux.HandleFunc(repo+".atom", func(w http.ResponseWriter, r *http.Request) {
		var b strings.Builder
		b.WriteString(`<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom">`)
		for _, tag := range f.atomTags {
			fmt.Fprintf(&b, `<entry><title>%s</title><link rel="alternate" href="https://github.com/o/r/releases/tag/%s"/><content type="html">notes of %s</content></entry>`, tag, tag, tag)
		}
		b.WriteString(`</feed>`)
		w.Header().Set("Content-Type", "application/atom+xml")
		w.Write([]byte(b.String()))
	})
	mux.HandleFunc(repo+"/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name":%q}`, f.latestTag)
	})
	f.manifest = func(tag string) string {
		ver := strings.TrimPrefix(tag, "v")
		return fmt.Sprintf("version: %s\nfiles:\n  - url: GuiZhi-%s-x64-setup.exe\n    sha512: %s\n    size: %d\npath: GuiZhi-%s-x64-setup.exe\nsha512: %s\nreleaseDate: '2026-11-01T00:00:00.000Z'\n",
			ver, ver, sha512b64(f.installer), len(f.installer), ver, sha512b64(f.installer))
	}
	mux.HandleFunc(repo+"/download/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, repo+"/download/")
		parts := strings.SplitN(rest, "/", 2)
		tag, file := parts[0], parts[1]
		switch {
		case strings.HasSuffix(file, ".yml"):
			if (f.ymlOK != nil && !f.ymlOK(tag, file)) || (f.ymlOK == nil && file != "latest.yml" && file != "beta.yml") {
				http.NotFound(w, r)
				return
			}
			w.Write([]byte(f.manifest(tag)))
		case strings.HasSuffix(file, ".exe"):
			f.exeHits.Add(1)
			w.Header().Set("Content-Length", fmt.Sprint(len(f.installer)))
			w.Write(f.installer)
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/repos/"+updaterOwner+"/"+updaterRepo+"/releases", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(f.apiReleases))
	})
	mux.HandleFunc("/mirror/latest.yml", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(f.manifest("v0.31.0")))
	})
	mux.HandleFunc("/mirror/GuiZhi-0.31.0-x64-setup.exe", func(w http.ResponseWriter, r *http.Request) { w.Write(f.installer) })
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)

	f.u = newUpdater(updaterDeps{
		client: f.srv.Client(), githubBase: f.srv.URL, apiBase: f.srv.URL,
		currentVersion: func() string { return current },
		isDev:          func() bool { return false },
		cacheDir:       filepath.Join(f.dir, "guizhi-updater"),
		push: func(ch string, p any) {
			if ch != "updater:status" {
				return
			}
			f.mu.Lock()
			f.statuses = append(f.statuses, p.(map[string]any))
			f.mu.Unlock()
		},
		openExternal: func(s string) error { f.opened = append(f.opened, s); return nil },
		openPath:     func(s string) error { f.opened = append(f.opened, s); return nil },
		showInFolder: func(s string) { f.revealed = append(f.revealed, s) },
		downloadsDir: func() string { return filepath.Join(f.dir, "Downloads") },
		spawnInstaller: func(p string, args []string) error {
			f.spawned = append(f.spawned, append([]string{p}, args...))
			return nil
		},
		quit:          func() { f.quit <- struct{}{} },
		progressEvery: time.Millisecond,
		mirrors: func(channel, tag string) []string {
			return []string{f.srv.URL + "/dead", f.srv.URL + "/mirror"}
		},
	})
	return f
}

func (f *updaterFixture) statusNames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, s := range f.statuses {
		out = append(out, s["status"].(string))
	}
	return out
}

func rawOpts(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

func TestUpdaterCheckDownloadInstall(t *testing.T) {
	f := newUpdaterFixture(t, "0.29.0")
	ctx := context.Background()

	res := f.u.Check(ctx, rawOpts(map[string]any{"channel": "stable", "trigger": "startup"}))
	if res["success"] != true || res["updateAvailable"] != true {
		t.Fatalf("check = %v", res)
	}
	if got := res["result"].(map[string]any)["updateInfo"].(map[string]any)["version"]; got != "0.30.0" {
		t.Fatalf("version = %v", got)
	}
	if names := f.statusNames(); strings.Join(names, ",") != "checking,available" {
		t.Fatalf("statuses = %v", names)
	}
	info := f.statuses[1]["info"].(simpleUpdateInfo)
	if info.Version != "0.30.0" || info.ReleaseNotes != "notes of v0.30.0" || info.ReleaseDate != "2026-11-01T00:00:00.000Z" {
		t.Fatalf("info = %+v", info)
	}

	// 下载：进度单调，完成后推送 downloaded，文件校验和一致
	f.statuses = nil
	if r := f.u.Download(ctx, rawOpts(map[string]any{"channel": "stable"})); r["success"] != true {
		t.Fatalf("download = %v", r)
	}
	names := f.statusNames()
	if names[len(names)-1] != "downloaded" || names[0] != "downloading" {
		t.Fatalf("download statuses = %v", names)
	}
	prev := -1.0
	for _, s := range f.statuses {
		if s["status"] == "downloading" {
			p := s["progress"].(map[string]any)["percent"].(float64)
			if p < prev {
				t.Fatalf("progress regressed %v -> %v", prev, p)
			}
			prev = p
		}
	}
	if prev != 100 {
		t.Fatalf("final percent = %v", prev)
	}
	dest := f.u.installerPath
	if sum, _ := fileSHA512(dest); sum != sha512b64(f.installer) {
		t.Fatalf("downloaded file checksum mismatch")
	}
	if f.exeHits.Load() != 1 {
		t.Fatalf("hits = %d", f.exeHits.Load())
	}

	// 再次下载命中缓存：不再请求安装包
	f.statuses = nil
	if r := f.u.Download(ctx, rawOpts(map[string]any{"channel": "stable"})); r["success"] != true {
		t.Fatalf("download2 = %v", r)
	}
	if f.exeHits.Load() != 1 {
		t.Fatalf("cache miss: hits = %d", f.exeHits.Load())
	}
	if names := f.statusNames(); strings.Join(names, ",") != "downloaded" {
		t.Fatalf("cached statuses = %v", names)
	}

	// 打开已下载文件：定位到文件
	if r := f.u.OpenDownloaded(); r["success"] != true || r["path"] != dest || len(f.revealed) != 1 {
		t.Fatalf("openDownloaded = %v", r)
	}

	// 安装：先备份，再以 --updated --force-run 启动安装包并退出
	var backups []string
	f.u.d.createBackup = func(reason string) { backups = append(backups, reason) }
	r := f.u.Install()
	if r["success"] != true || r["manual"] != false {
		t.Fatalf("install = %v", r)
	}
	if len(backups) != 1 || backups[0] != "pre-update" {
		t.Fatalf("backups = %v", backups)
	}
	if len(f.spawned) != 1 || f.spawned[0][0] != dest || f.spawned[0][1] != "--updated" || f.spawned[0][2] != "--force-run" {
		t.Fatalf("spawned = %v", f.spawned)
	}
	select {
	case <-f.quit:
	case <-time.After(2 * time.Second):
		t.Fatal("安装后未退出")
	}
	if r := f.u.Install(); r["success"] != false {
		t.Fatalf("重复安装应被拒绝: %v", r)
	}
}

func TestUpdaterNotAvailableAndErrors(t *testing.T) {
	ctx := context.Background()
	f := newUpdaterFixture(t, "0.30.0")
	res := f.u.Check(ctx, nil)
	if res["success"] != true || res["updateAvailable"] != false {
		t.Fatalf("同版本不应有更新: %v", res)
	}
	if names := f.statusNames(); names[len(names)-1] != "not-available" {
		t.Fatalf("statuses = %v", names)
	}

	// 未检查就下载 / 安装
	g := newUpdaterFixture(t, "0.29.0")
	if r := g.u.Download(ctx, nil); r["success"] != false || !strings.Contains(r["error"].(string), "Please check update first") {
		t.Fatalf("download without check = %v", r)
	}
	if r := g.u.Install(); r["success"] != false || len(g.spawned) != 0 {
		t.Fatalf("install without download = %v", r)
	}
	if r := g.u.OpenDownloaded(); r["success"] != false || r["error"] != "No downloaded update file is available" {
		t.Fatalf("openDownloaded = %v", r)
	}

	// latest.yml 404：状态里是归一化的中文文案，返回值带前缀
	g.manifest = nil
	g.latestTag = "v0.99.0"
	g.atomTags = []string{"v0.99.0"}
	h := newUpdaterFixture(t, "0.29.0")
	h.atomTags = []string{"v0.30.0"}
	h.srv.Config.Handler = http.NotFoundHandler()
	r := h.u.Check(ctx, nil)
	if r["success"] != false || !strings.HasPrefix(r["error"].(string), "Update check failed: ") {
		t.Fatalf("404 check = %v", r)
	}
	last := h.statuses[len(h.statuses)-1]
	if last["status"] != "error" || !strings.Contains(last["error"].(string), "更新检查失败") {
		t.Fatalf("error status = %v", last)
	}

	// 开发模式
	f.u.d.isDev = func() bool { return true }
	if r := f.u.Check(ctx, nil); r["devDisabled"] != true || r["success"] != false {
		t.Fatalf("dev check = %v", r)
	}
	if r := f.u.Download(ctx, nil); r["error"] != "Download disabled in development mode" {
		t.Fatalf("dev download = %v", r)
	}
	if r := f.u.Install(); r["error"] != "Install disabled in development mode" {
		t.Fatalf("dev install = %v", r)
	}
}

func TestUpdaterChecksumMismatch(t *testing.T) {
	f := newUpdaterFixture(t, "0.29.0")
	ctx := context.Background()
	good := f.manifest
	f.manifest = func(tag string) string {
		return strings.ReplaceAll(good(tag), sha512b64(f.installer), sha512b64([]byte("other")))
	}
	if r := f.u.Check(ctx, nil); r["success"] != true {
		t.Fatalf("check = %v", r)
	}
	r := f.u.Download(ctx, nil)
	if r["success"] != false || !strings.Contains(r["error"].(string), "sha512 checksum mismatch") {
		t.Fatalf("download = %v", r)
	}
	if f.u.installerPath != "" {
		t.Fatalf("校验失败不应记录安装包路径")
	}
	last := f.statuses[len(f.statuses)-1]
	if last["status"] != "error" || !strings.Contains(last["error"].(string), "SHA512 校验失败") {
		t.Fatalf("status = %v", last)
	}
	entries, _ := os.ReadDir(f.u.pendingDir())
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".exe") || strings.HasSuffix(e.Name(), ".part") {
			t.Fatalf("校验失败后残留文件 %s", e.Name())
		}
	}
}

func TestUpdaterMirrorFallsThrough(t *testing.T) {
	f := newUpdaterFixture(t, "0.29.0")
	ctx := context.Background()
	res := f.u.Check(ctx, rawOpts(true))
	if res["success"] != true || res["updateAvailable"] != true {
		t.Fatalf("mirror check = %v", res)
	}
	// 第一个镜像失败会各推一次 error 状态，最终成功
	names := f.statusNames()
	if strings.Join(names, ",") != "checking,error,checking,available" {
		t.Fatalf("statuses = %v", names)
	}
	if r := f.u.Download(ctx, rawOpts(true)); r["success"] != true {
		t.Fatalf("mirror download = %v", r)
	}
	if f.u.installerPath == "" {
		t.Fatal("镜像下载后应有安装包路径")
	}

	// 全部镜像失败
	g := newUpdaterFixture(t, "0.29.0")
	g.u.d.mirrors = func(string, string) []string { return []string{g.srv.URL + "/dead1", g.srv.URL + "/dead2"} }
	if r := g.u.Check(ctx, rawOpts(true)); r["success"] != false || r["error"] != "All mirror sources failed. Please try disabling mirror acceleration." {
		t.Fatalf("all mirrors failed = %v", r)
	}
}

func TestUpdaterPreviewChannel(t *testing.T) {
	f := newUpdaterFixture(t, "0.30.0")
	ctx := context.Background()
	f.atomTags = []string{"v0.31.0-beta.2", "v0.31.0-beta.1", "v0.30.0"}
	f.apiReleases = `[{"tag_name":"v0.31.0-beta.2","prerelease":true,"draft":false},{"tag_name":"v0.30.0","prerelease":false,"draft":false}]`
	res := f.u.Check(ctx, rawOpts(map[string]any{"channel": "preview"}))
	if res["success"] != true || res["updateAvailable"] != true {
		t.Fatalf("preview check = %v", res)
	}
	if v := res["result"].(map[string]any)["updateInfo"].(map[string]any)["version"]; v != "0.31.0-beta.2" {
		t.Fatalf("version = %v", v)
	}
	if f.u.lastFeed.channel != "preview" || f.u.lastFeed.tag != "v0.31.0-beta.2" {
		t.Fatalf("feed = %+v", f.u.lastFeed)
	}

	// 预览通道查询失败
	f.apiReleases = `[]`
	r := f.u.Check(ctx, rawOpts(map[string]any{"channel": "preview", "trigger": "manual"}))
	if r["success"] != false {
		t.Fatalf("empty preview = %v", r)
	}
}

func TestUpdaterMiscHandlers(t *testing.T) {
	f := newUpdaterFixture(t, "0.29.0")
	b := &Bridge{handlers: map[string]handler{}}
	f.u.register(b)
	call := func(ch string) any {
		v, err := b.handlers[ch](context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	if call("updater:platform") != nodePlatform() || call("updater:installSource") != "unknown" {
		t.Fatal("platform/installSource")
	}
	call("updater:openReleases")
	if len(f.opened) != 1 || f.opened[0] != "https://github.com/Couleur-Share/GuiZhi/releases" {
		t.Fatalf("opened = %v", f.opened)
	}
	r := call("updater:openDownloadedUpdate").(map[string]any)
	if r["success"] != false || r["path"] != filepath.Join(f.dir, "Downloads") {
		t.Fatalf("openDownloaded = %v", r)
	}
	for _, ch := range []string{"check", "download", "install"} {
		if b.handlers["updater:"+ch] == nil {
			t.Fatalf("缺少 updater:%s", ch)
		}
	}
}

func TestNormalizeUpdateOptions(t *testing.T) {
	cases := []struct {
		in   string
		want updateRequest
	}{
		{``, updateRequest{false, "stable", "manual"}},
		{`true`, updateRequest{true, "stable", "manual"}},
		{`{"useMirror":1,"channel":"preview","trigger":"interval"}`, updateRequest{true, "preview", "interval"}},
		{`{"channel":"nightly","trigger":"x"}`, updateRequest{false, "stable", "manual"}},
		{`"str"`, updateRequest{false, "stable", "manual"}},
	}
	for _, c := range cases {
		if got := normalizeUpdateOptions(json.RawMessage(c.in)); got != c.want {
			t.Errorf("%q: got %+v want %+v", c.in, got, c.want)
		}
	}
}
