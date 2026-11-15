package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// webview2Fixture 是真实 WebView2 写出的 Local Storage（testdata/make-webview2-localstorage.mjs 生成）：
// leveldb 目录、页面自己读回的全部键值（expected.json）与 meta.json。
type webview2Fixture struct {
	Dir      string
	Origin   string
	Entries  map[string]string
	RelPath  string
	Browser  string
	SeedKeys []string
}

func loadWebView2Fixture(t *testing.T) webview2Fixture {
	t.Helper()
	dir := filepath.Join("testdata", "webview2-localstorage")
	var meta struct {
		Origin              string   `json:"origin"`
		LevelDBRelativePath string   `json:"leveldbRelativePath"`
		BrowserVersion      string   `json:"browserVersion"`
		SeedKeys            []string `json:"seedKeys"`
	}
	var expected struct {
		Origin  string            `json:"origin"`
		Entries map[string]string `json:"entries"`
	}
	for file, into := range map[string]any{"meta.json": &meta, "expected.json": &expected} {
		raw, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			t.Fatalf("读取夹具 %s 失败（用 testdata/make-webview2-localstorage.mjs 重新生成）：%v", file, err)
		}
		if err := json.Unmarshal(raw, into); err != nil {
			t.Fatalf("解析夹具 %s 失败：%v", file, err)
		}
	}
	if meta.Origin != expected.Origin {
		t.Fatalf("夹具自相矛盾：meta.origin=%q expected.origin=%q", meta.Origin, expected.Origin)
	}
	return webview2Fixture{Dir: dir, Origin: meta.Origin, Entries: expected.Entries, RelPath: meta.LevelDBRelativePath, Browser: meta.BrowserVersion, SeedKeys: meta.SeedKeys}
}

// installWebView2Fixture 把夹具的 leveldb 放进一个临时的 WebView2 用户数据目录，返回该目录。
func installWebView2Fixture(t *testing.T, fx webview2Fixture) string {
	t.Helper()
	folder := t.TempDir()
	src := filepath.Join(fx.Dir, filepath.FromSlash(fx.RelPath))
	dst := filepath.Join(folder, webview2LocalStorageRel)
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyDirFlat(src, dst); err != nil {
		t.Fatalf("复制夹具失败：%v", err)
	}
	return folder
}

// 夹具记录的位置与 origin 必须和定位逻辑一致：WebView2 内核升级后若换了位置，这里先红。
func TestWebView2FixtureMatchesLocatorFacts(t *testing.T) {
	fx := loadWebView2Fixture(t)
	if want := filepath.ToSlash(webview2LocalStorageRel); fx.RelPath != want {
		t.Errorf("夹具里的 leveldb 相对路径 %q 与定位逻辑 %q 不一致", fx.RelPath, want)
	}
	if fx.Origin != rendererOrigin {
		t.Errorf("夹具里的页面 origin %q 与 rendererOrigin %q 不一致", fx.Origin, rendererOrigin)
	}
	if !strings.Contains(fx.Browser, ".") {
		t.Errorf("夹具应记录 WebView2 内核版本：%q", fx.Browser)
	}
}

func TestReadWebView2LocalStorageFromRealFixture(t *testing.T) {
	fx := loadWebView2Fixture(t)
	folder := installWebView2Fixture(t, fx)

	got, err := readWebView2LocalStorage(folder)
	if err != nil {
		t.Fatalf("读取失败：%v", err)
	}
	if got == nil {
		t.Fatal("应读到归知页面的条目，实际为 nil")
	}
	if got.Origin != rendererOrigin {
		t.Errorf("origin = %q，期望 %q", got.Origin, rendererOrigin)
	}
	if !reflect.DeepEqual(got.Entries, fx.Entries) {
		t.Fatalf("读出的键值与页面读回的不一致：\n得到 %v\n期望 %v", got.Entries, fx.Entries)
	}
	// 种子里同时有 Latin1（纯 ASCII）与 UTF-16（含中文）编码的键和值，都要还原
	if got.Entries["无关键-fixture"] != "与归知无关的值" || got.Entries["wechat-reader-width"] != "wide" {
		t.Errorf("两种编码的键值没有都还原：%q / %q", got.Entries["无关键-fixture"], got.Entries["wechat-reader-width"])
	}
	for _, key := range fx.SeedKeys {
		if _, ok := got.Entries[key]; !ok {
			t.Errorf("缺少种子键 %q", key)
		}
	}
}

// 读取不能改动原目录，也不能留下 LOCK 之类的痕迹（浏览器可能正在运行）。
func TestReadWebView2LocalStorageDoesNotTouchSource(t *testing.T) {
	fx := loadWebView2Fixture(t)
	folder := installWebView2Fixture(t, fx)
	dir := filepath.Join(folder, webview2LocalStorageRel)
	snapshot := func() map[string]string {
		out := map[string]string{}
		ents, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range ents {
			b, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			out[e.Name()] = string(b)
		}
		return out
	}
	before := snapshot()
	if _, err := readWebView2LocalStorage(folder); err != nil {
		t.Fatal(err)
	}
	if after := snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatalf("读取改动了原目录：前 %d 个文件，后 %d 个文件", len(before), len(after))
	}
}

func TestReadWebView2LocalStorageMissingAndCorrupt(t *testing.T) {
	if got, err := readWebView2LocalStorage(t.TempDir()); got != nil || err != nil {
		t.Fatalf("没有 WebView2 数据应得到 (nil, nil)：%v %v", got, err)
	}
	folder := t.TempDir()
	dir := filepath.Join(folder, webview2LocalStorageRel)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"CURRENT", "MANIFEST-000001"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("garbage\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := readWebView2LocalStorage(folder)
	if err == nil || got != nil {
		t.Fatalf("损坏的库应返回错误：%v %v", got, err)
	}
	if !strings.Contains(err.Error(), "WebView2 Local Storage") {
		t.Errorf("错误里应说明是哪个库：%v", err)
	}
}

func TestPickWebView2Origin(t *testing.T) {
	settingsKey := legacyLSSettingsKey
	cases := []struct {
		name   string
		in     map[string]map[string]string
		origin string // 空表示 nil
	}{
		{"渲染层 origin 优先", map[string]map[string]string{
			rendererOrigin: {"a": "1"}, "http://localhost:5173": {settingsKey: "{}"},
		}, rendererOrigin},
		{"渲染层 origin 为空时看别的带设置的", map[string]map[string]string{
			rendererOrigin: {}, "http://localhost:5173": {settingsKey: "{}"},
		}, "http://localhost:5173"},
		{"无关来源不导入", map[string]map[string]string{"https://example.com": {"x": "1"}}, ""},
		{"多个带设置的取字典序最小", map[string]map[string]string{
			"http://b.test": {settingsKey: "{}"}, "http://a.test": {settingsKey: "{}"},
		}, "http://a.test"},
		{"没有数据", map[string]map[string]string{}, ""},
	}
	for _, c := range cases {
		got := pickWebView2Origin(c.in)
		switch {
		case c.origin == "" && got != nil:
			t.Errorf("%s：期望 nil，得到 %q", c.name, got.Origin)
		case c.origin != "" && (got == nil || got.Origin != c.origin):
			t.Errorf("%s：期望 %q，得到 %v", c.name, c.origin, got)
		}
	}
}

func TestWebView2UserDataFolderFollowsMyGo(t *testing.T) {
	exe := func() string { return `C:\apps\guizhi` }
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"环境变量给了绝对路径", map[string]string{"WEBVIEW2_USER_DATA_FOLDER": `D:\wv2`, "LOCALAPPDATA": `C:\L`}, `D:\wv2`},
		{"相对路径相对可执行文件目录", map[string]string{"WEBVIEW2_USER_DATA_FOLDER": `wv2`}, filepath.Join(`C:\apps\guizhi`, "wv2")},
		{"默认在 LOCALAPPDATA 下按应用名", map[string]string{"LOCALAPPDATA": `C:\L`}, filepath.Join(`C:\L`, "GuiZhi", "WebView2")},
		{"没有 LOCALAPPDATA 用临时目录", map[string]string{}, filepath.Join(os.TempDir(), "GuiZhi", "WebView2")},
	}
	for _, c := range cases {
		if got := webview2UserDataFolder(env(c.env), exe); got != c.want {
			t.Errorf("%s：得到 %q，期望 %q", c.name, got, c.want)
		}
	}
}

func TestSanitizeWebView2NameFollowsMyGo(t *testing.T) {
	cases := map[string]string{"GuiZhi": "GuiZhi", "a/b:c": "a_b_c", `x\y|z?*"<>`: "x_y_z_____", "": "MyGo", "归知": "归知", "a\x01b": "a_b"}
	for in, want := range cases {
		if got := sanitizeWebView2Name(in); got != want {
			t.Errorf("sanitizeWebView2Name(%q) = %q，期望 %q", in, got, want)
		}
	}
}

// MyGo 用应用名决定 %LOCALAPPDATA% 下的目录：改了 mygo.json 的 name 而忘了这里，就会读错地方。
func TestWebView2AppNameMatchesMyGoJSON(t *testing.T) {
	raw, err := os.ReadFile("mygo.json")
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Name != webview2AppName {
		t.Fatalf("mygo.json 的 name=%q，webview2AppName=%q：WebView2 用户数据目录的定位会读错地方", cfg.Name, webview2AppName)
	}
}
