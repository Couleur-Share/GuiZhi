package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/dop251/goja"
	"github.com/syndtr/goleveldb/leveldb"
)

// 夹具来自真实 Electron 45 写出的 Local Storage（testdata/make-legacy-localstorage.cjs，file:// 页面）：
// 键值同时覆盖 Latin1 与 UTF-16 两种 Chromium 编码。
func copyFixtureUserData(t *testing.T) string {
	t.Helper()
	src := filepath.Join("testdata", "legacy-localstorage", "Local Storage", "leveldb")
	ud := t.TempDir()
	dst := filepath.Join(ud, "Local Storage", "leveldb")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	ents, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		b, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return ud
}

func TestReadLegacyLocalStorageFromRealElectronFixture(t *testing.T) {
	ud := copyFixtureUserData(t)
	got, err := readLegacyLocalStorage(ud)
	if err != nil {
		t.Fatalf("读取失败：%v", err)
	}
	if got == nil {
		t.Fatal("应读到 file:// 的条目，实际为 nil")
	}
	if got.Origin != "file://" {
		t.Errorf("origin = %q，期望 file://", got.Origin)
	}
	want := map[string]string{
		"guizhi-settings":        `{"state":{"language":"zh","theme":"dark","note":"归知"},"version":3}`,
		"guizhi-setup-dismissed": "1",
		"视图-列配置":                 "标题,标签,来源",
		"emoji":                  "a😀b",
		"latin1-high":            "café",
		"empty":                  "",
		"lone-surrogate":         "x\uFFFDy", // Go 字符串放不下孤立代理项，按 U+FFFD 处理（已接受的差异）
	}
	for k, v := range want {
		if gv, ok := got.Entries[k]; !ok || gv != v {
			t.Errorf("键 %q：得到 %q（存在=%v），期望 %q", k, gv, ok, v)
		}
	}
	if big := got.Entries["big"]; len(big) != 100000 || strings.Trim(big, "z") != "" {
		t.Errorf("big 值长度 %d，期望 100000 个 z（跨 32KB 日志块的记录要能还原）", len(big))
	}
	if len(got.Entries) != len(want)+1 {
		t.Errorf("条目数 %d，期望 %d", len(got.Entries), len(want)+1)
	}
}

func TestReadLegacyLocalStorageDoesNotTouchSourceAndWorksWhileLocked(t *testing.T) {
	ud := copyFixtureUserData(t)
	dir := filepath.Join(ud, "Local Storage", "leveldb")
	// 旧版 Electron 可能还开着：模拟 LOCK 文件存在（且被占用时也不能影响读取）。
	lock, err := os.OpenFile(filepath.Join(dir, "LOCK"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	before := dirFingerprint(t, dir)
	if _, err := readLegacyLocalStorage(ud); err != nil {
		t.Fatalf("读取失败：%v", err)
	}
	if after := dirFingerprint(t, dir); after != before {
		t.Errorf("读取不应改动旧数据目录\n之前：%s\n之后：%s", before, after)
	}
}

func dirFingerprint(t *testing.T, dir string) string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	for _, e := range ents {
		info, _ := e.Info()
		sb.WriteString(fmt.Sprintf("%s:%s:%d;", e.Name(), info.ModTime(), info.Size()))
	}
	return sb.String()
}

func TestReadLegacyLocalStorageMissingDirIsNotAnError(t *testing.T) {
	got, err := readLegacyLocalStorage(t.TempDir())
	if err != nil || got != nil {
		t.Fatalf("没有旧数据应返回 (nil, nil)，得到 (%v, %v)", got, err)
	}
}

func TestReadLegacyLocalStorageCorruptDirReturnsError(t *testing.T) {
	ud := t.TempDir()
	dir := filepath.Join(ud, "Local Storage", "leveldb")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "CURRENT"), []byte("MANIFEST-000001\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "MANIFEST-000001"), []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readLegacyLocalStorage(ud)
	if err == nil || got != nil {
		t.Fatalf("损坏的旧数据应返回错误（调用方据此跳过导入而不是阻止启动），得到 (%v, %v)", got, err)
	}
}

// 用 goleveldb 写一个合成库，覆盖真实夹具里没有的情况：多个 origin 的挑选。
func makeSyntheticLS(t *testing.T, kv map[string]string) string {
	t.Helper()
	ud := t.TempDir()
	dir := filepath.Join(ud, "Local Storage", "leveldb")
	db, err := leveldb.OpenFile(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range kv {
		if err := db.Put([]byte(k), []byte(v), nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return ud
}

func TestReadLegacyLocalStoragePicksOrigin(t *testing.T) {
	const l1 = "\x01" // Chromium：Latin1 编码标记
	cases := []struct {
		name       string
		kv         map[string]string
		wantOrigin string // 空 = 期望 nil
		wantKey    string
	}{
		{"仅 file://", map[string]string{"_file://\x00" + l1 + "guizhi-settings": l1 + "{}"}, "file://", "guizhi-settings"},
		{"file:// 与开发服务器都有：优先 file://", map[string]string{
			"_file://\x00" + l1 + "guizhi-settings":               l1 + "prod",
			"_http://localhost:5173\x00" + l1 + "guizhi-settings": l1 + "dev",
		}, "file://", "guizhi-settings"},
		{"file:// 没有设置、开发服务器有：用有设置的那个", map[string]string{
			"_file://\x00" + l1 + "other":                         l1 + "x",
			"_http://localhost:5173\x00" + l1 + "guizhi-settings": l1 + "dev",
		}, "http://localhost:5173", "guizhi-settings"},
		{"file:// 有数据但没设置、别处也没有：仍取 file://", map[string]string{"_file://\x00" + l1 + "other": l1 + "x"}, "file://", "other"},
		{"只有无关 origin：不导入", map[string]string{"_https://example.com\x00" + l1 + "other": l1 + "x"}, "", ""},
		{"只有元数据键：不导入", map[string]string{"META:file://": "m", "VERSION": "1"}, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := readLegacyLocalStorage(makeSyntheticLS(t, c.kv))
			if err != nil {
				t.Fatal(err)
			}
			if c.wantOrigin == "" {
				if got != nil {
					t.Fatalf("期望不导入，得到 origin=%q", got.Origin)
				}
				return
			}
			if got == nil || got.Origin != c.wantOrigin {
				t.Fatalf("origin 期望 %q，得到 %+v", c.wantOrigin, got)
			}
			if _, ok := got.Entries[c.wantKey]; !ok {
				t.Errorf("缺少键 %q：%v", c.wantKey, got.Entries)
			}
		})
	}
}

// ---- 注入脚本：放进 goja 里跑，替身 location / localStorage / window ----

type lsHarness struct {
	vm        *goja.Runtime
	store     map[string]string
	confirmed int
}

func runLegacyScript(t *testing.T, origin string, existing map[string]string, entries map[string]string) *lsHarness {
	t.Helper()
	h := &lsHarness{vm: goja.New(), store: map[string]string{}}
	for k, v := range existing {
		h.store[k] = v
	}
	ls := h.vm.NewObject()
	_ = ls.Set("getItem", func(k string) any {
		if v, ok := h.store[k]; ok {
			return v
		}
		return nil
	})
	_ = ls.Set("setItem", func(k, v string) { h.store[k] = v })
	_ = h.vm.Set("localStorage", ls)
	_ = h.vm.Set("location", map[string]any{"origin": origin})
	_ = h.vm.Set("document", map[string]any{"readyState": "complete"})
	_ = h.vm.Set("console", map[string]any{"error": func(...any) {}})
	mygo := h.vm.NewObject()
	_ = mygo.Set("call", func(method string, args ...any) any {
		if method == "Bridge.Invoke" && len(args) > 0 && args[0] == "legacyLocalStorage:imported" {
			h.confirmed++
		}
		p, resolve, _ := h.vm.NewPromise()
		_ = resolve(goja.Undefined())
		return p
	})
	win := h.vm.NewObject()
	_ = win.Set("mygo", mygo)
	_ = win.Set("addEventListener", func(string, goja.Value) {})
	_ = h.vm.Set("window", win)
	script := legacyLocalStorageScript(&legacyLocalStorage{Origin: "file://", Entries: entries}, rendererOrigin)
	if _, err := h.vm.RunString(script); err != nil {
		t.Fatalf("脚本执行失败：%v\n%s", err, script)
	}
	return h
}

func TestLegacyScriptAppliesOnlyAbsentKeysOnceAndConfirms(t *testing.T) {
	entries := map[string]string{"guizhi-settings": `{"a":"归知"}`, "k2": "v2", "emoji": "a😀b"}
	h := runLegacyScript(t, rendererOrigin, map[string]string{"k2": "已有值"}, entries)
	if h.store["guizhi-settings"] != `{"a":"归知"}` || h.store["emoji"] != "a😀b" {
		t.Errorf("缺失的键应写入：%v", h.store)
	}
	if h.store["k2"] != "已有值" {
		t.Errorf("已有的键不能被覆盖：%q", h.store["k2"])
	}
	if h.store[legacyLSMarkerKey] == "" {
		t.Error("应写入 localStorage 标记键，避免之后重复导入")
	}
	if h.confirmed != 1 {
		t.Errorf("应向主进程确认一次，实际 %d", h.confirmed)
	}
}

func TestLegacyScriptSkipsWhenMarkerPresentButStillConfirms(t *testing.T) {
	h := runLegacyScript(t, rendererOrigin, map[string]string{legacyLSMarkerKey: "1"}, map[string]string{"x": "1"})
	if _, ok := h.store["x"]; ok {
		t.Error("已有标记时不应再次导入（用户可能已删掉或改过这些键）")
	}
	if h.confirmed != 1 {
		t.Errorf("上次确认可能没送达，这次仍应确认，实际 %d", h.confirmed)
	}
}

func TestLegacyScriptIgnoresOtherOrigins(t *testing.T) {
	// 预加载脚本也会进入子框架/其他来源；它们不是渲染进程主页面，不能写入也不能确认。
	h := runLegacyScript(t, "https://evil.example", nil, map[string]string{"x": "1"})
	if len(h.store) != 0 || h.confirmed != 0 {
		t.Errorf("其他 origin 应完全不动：store=%v confirmed=%d", h.store, h.confirmed)
	}
}

func TestLegacyImportMarkerFile(t *testing.T) {
	ud := t.TempDir()
	if !legacyImportPending(ud) {
		t.Fatal("没有标记文件时应为待导入")
	}
	if err := markLegacyImported(ud); err != nil {
		t.Fatal(err)
	}
	if legacyImportPending(ud) {
		t.Error("写入标记后不应再待导入")
	}
}

// 可选：用真实 userData（含已压缩的 .ldb / snappy 块）验证读取，只打印键名与数量，不打印值。
// 用法：$env:GUIZHI_REAL_LS_USERDATA = "$env:APPDATA\GuiZhi"; go test -run RealUserData -v .
func TestReadLegacyLocalStorageRealUserData(t *testing.T) {
	ud := os.Getenv("GUIZHI_REAL_LS_USERDATA")
	if ud == "" {
		t.Skip("未设置 GUIZHI_REAL_LS_USERDATA")
	}
	got, err := readLegacyLocalStorage(ud)
	if err != nil {
		t.Fatalf("读取失败：%v", err)
	}
	if got == nil {
		t.Fatal("没有读到任何可导入的条目")
	}
	keys := make([]string, 0, len(got.Entries))
	total := 0
	for k, v := range got.Entries {
		keys = append(keys, k)
		total += len(v)
	}
	sort.Strings(keys)
	t.Logf("origin=%s，%d 个键，值总长 %d 字节；键名：%v", got.Origin, len(keys), total, keys)
}
