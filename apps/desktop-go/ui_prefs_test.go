package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"guizhi-mygo-shell/native/settings"
)

func prefsPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "config", uiPrefsFileName)
}

func readPrefsFile(t *testing.T, path string) map[string]json.RawMessage {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败：%v", path, err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("%s 不是合法 JSON：%v\n%s", path, err, raw)
	}
	return top
}

// compactRaw 去掉排版：文件是缩进写出的，断言比较的是 JSON 内容。
func compactRaw(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var b bytes.Buffer
	if err := json.Compact(&b, raw); err != nil {
		t.Fatalf("不是合法 JSON：%v\n%s", err, raw)
	}
	return b.String()
}

func TestUIPrefsStoreStartsEmptyWithDefaults(t *testing.T) {
	path := prefsPath(t)
	s := openUIPrefsStore(path, nil)
	if s.Initialized() || s.Import() != nil {
		t.Fatal("没有文件时不该已初始化")
	}
	if !s.UI().Equal(settings.DefaultUI()) {
		t.Fatalf("没有文件时界面设置应是默认值：%+v", s.UI())
	}
	if _, ok := s.Get("ui.sidebarPanelWidth"); ok {
		t.Fatal("没有文件时不该有偏好")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("只读打开不能创建文件")
	}
}

func TestUIPrefsStoreInitializeAndReopen(t *testing.T) {
	path := prefsPath(t)
	s := openUIPrefsStore(path, nil)
	ui := settings.DefaultUI()
	ui.ThemeMode, ui.ThemeHue = settings.ThemeDark, 150
	uiRaw, err := settings.Keys(ui)
	if err != nil {
		t.Fatal(err)
	}
	rec := uiPrefsImport{Source: "webview2", Origin: rendererOrigin, At: "2026-11-15T00:00:00Z", Keys: 3, Issues: []string{"x：原因"}}
	if err := s.initialize(uiRaw, map[string]json.RawMessage{"ui.sidebarPanelWidth": json.RawMessage("333")}, rec); err != nil {
		t.Fatal(err)
	}
	if !s.Initialized() {
		t.Fatal("写出后应已初始化")
	}
	if err := s.initialize(nil, nil, rec); err == nil {
		t.Fatal("已初始化后再初始化应报错：导入只发生一次")
	}

	top := readPrefsFile(t, path)
	for _, key := range []string{"version", "settings", "prefs", "import"} {
		if _, ok := top[key]; !ok {
			t.Errorf("文件缺少顶层键 %q", key)
		}
	}
	if string(top["version"]) != "1" {
		t.Errorf("version = %s", top["version"])
	}

	// 重新打开：内容一致
	r := openUIPrefsStore(path, nil)
	if !r.Initialized() {
		t.Fatal("重新打开应已初始化")
	}
	if got := r.UI(); !got.Equal(ui) {
		t.Fatalf("界面设置重开后不一致：\n%+v\n%+v", got, ui)
	}
	if raw, ok := r.Get("ui.sidebarPanelWidth"); !ok || string(raw) != "333" {
		t.Fatalf("偏好重开后不一致：%s %v", raw, ok)
	}
	if got := r.Import(); got == nil || got.Source != "webview2" || got.Keys != 3 || len(got.Issues) != 1 {
		t.Fatalf("导入记录重开后不一致：%+v", got)
	}
}

func TestUIPrefsStoreSetPersistsAndSkipsUnchanged(t *testing.T) {
	path := prefsPath(t)
	s := openUIPrefsStore(path, nil)
	if err := s.Set(settings.PrefSidebarPanelWidth, 300.5); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(settings.PrefLibraryColumns, json.RawMessage(`{"columns":[{"id":"title","visible":true,"width":300}]}`)); err != nil {
		t.Fatal(err)
	}
	if r := openUIPrefsStore(path, nil); !r.Initialized() {
		t.Fatal("Set 应在没有文件时创建它")
	} else if raw, _ := r.Get(settings.PrefSidebarPanelWidth); string(raw) != "300.5" {
		t.Fatalf("重开后读到 %s", raw)
	} else if raw, _ := r.Get(settings.PrefLibraryColumns); string(raw) != `{"columns":[{"id":"title","visible":true,"width":300}]}` {
		t.Fatalf("原始 JSON 偏好应原样保存：%s", raw)
	}

	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(40 * time.Millisecond)
	if err := s.Set(settings.PrefSidebarPanelWidth, 300.5); err != nil { // 同一个值
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("值没有变化时不该再写盘")
	}
}

func TestUIPrefsStoreRejectsUnencodableValue(t *testing.T) {
	s := openUIPrefsStore(prefsPath(t), nil)
	if err := s.Set("k", make(chan int)); err == nil || !strings.Contains(err.Error(), "k") {
		t.Fatalf("不可编码的值应报错并带上键名：%v", err)
	}
	if err := s.Set("k", json.RawMessage("{broken")); err == nil {
		t.Fatal("非法的原始 JSON 应报错")
	}
	if s.Initialized() {
		t.Fatal("失败的写入不该创建文件")
	}
}

// 新版本写进来的、本版本不认识的内容，保存时不能丢（降级后再升级不丢数据）。
func TestUIPrefsStoreKeepsUnknownContent(t *testing.T) {
	path := prefsPath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	src := `{"version":5,"future":{"a":[1,2]},"settings":{"themeMode":"dark","futureSetting":{"x":1}},"prefs":{"future.pref":true}}`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	s := openUIPrefsStore(path, nil)
	if got := s.UI().ThemeMode; got != settings.ThemeDark {
		t.Fatalf("应读出已知键：%s", got)
	}
	undo, err := s.ApplyUI(map[string]json.RawMessage{"fontSize": json.RawMessage(`"large"`)})
	if err != nil || undo == nil {
		t.Fatal(err)
	}
	if err := s.Set("new.pref", 1); err != nil {
		t.Fatal(err)
	}

	top := readPrefsFile(t, path)
	if string(top["version"]) != "5" {
		t.Errorf("不能把更新版本写的文件降级：version = %s", top["version"])
	}
	if compactRaw(t, top["future"]) != `{"a":[1,2]}` {
		t.Errorf("未识别的顶层键丢了：%s", top["future"])
	}
	var settingsMap, prefsMap map[string]json.RawMessage
	_ = json.Unmarshal(top["settings"], &settingsMap)
	_ = json.Unmarshal(top["prefs"], &prefsMap)
	if compactRaw(t, settingsMap["futureSetting"]) != `{"x":1}` || string(settingsMap["themeMode"]) != `"dark"` || string(settingsMap["fontSize"]) != `"large"` {
		t.Errorf("settings 段不对：%v", settingsMap)
	}
	if string(prefsMap["future.pref"]) != "true" || string(prefsMap["new.pref"]) != "1" {
		t.Errorf("prefs 段不对：%v", prefsMap)
	}
}

func TestUIPrefsStoreQuarantinesCorruptFiles(t *testing.T) {
	cases := map[string]string{
		"不是 JSON":       "{not json",
		"不是对象":          "[1,2,3]",
		"缺少 version":    `{"settings":{},"prefs":{}}`,
		"version 无效":    `{"version":0,"settings":{}}`,
		"settings 不是对象": `{"version":1,"settings":[1]}`,
		"prefs 不是对象":    `{"version":1,"prefs":"x"}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			path := prefsPath(t)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			var logged []string
			s := openUIPrefsStore(path, func(f string, a ...any) { logged = append(logged, fmt.Sprintf(f, a...)) })
			if s.Initialized() {
				t.Fatal("损坏的文件应按没有文件处理")
			}
			if !s.UI().Equal(settings.DefaultUI()) {
				t.Fatal("损坏的文件应得到默认界面设置")
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("损坏的文件应被改名挪开")
			}
			matches, _ := filepath.Glob(path + ".corrupt-*")
			if len(matches) != 1 {
				t.Fatalf("应留下一个 .corrupt-* 备份：%v", matches)
			}
			if b, _ := os.ReadFile(matches[0]); string(b) != content {
				t.Fatalf("备份应保留原内容：%q", b)
			}
			if len(logged) != 1 || !strings.Contains(logged[0], ".corrupt-") {
				t.Fatalf("应记一条日志说明改名去向：%v", logged)
			}
			// 之后可以正常写出新文件
			if err := s.Set("k", 1); err != nil {
				t.Fatal(err)
			}
			if !openUIPrefsStore(path, nil).Initialized() {
				t.Fatal("挪开损坏文件后应能写出新文件")
			}
		})
	}
}

func TestUIPrefsStoreIgnoresUnreadableImportRecord(t *testing.T) {
	path := prefsPath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":1,"settings":{"themeMode":"dark"},"import":"garbage"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s := openUIPrefsStore(path, nil)
	if !s.Initialized() || s.Import() != nil || s.UI().ThemeMode != settings.ThemeDark {
		t.Fatal("导入记录只是排查用的：读不懂就不要，不能让整份文件作废")
	}
}

func TestUIPrefsStoreApplyUIAndUndo(t *testing.T) {
	path := prefsPath(t)
	s := openUIPrefsStore(path, nil)

	// 文件原本不存在：撤销后恢复成没有文件
	undo, err := s.ApplyUI(map[string]json.RawMessage{"themeMode": json.RawMessage(`"dark"`)})
	if err != nil {
		t.Fatal(err)
	}
	if !s.Initialized() || s.UI().ThemeMode != settings.ThemeDark {
		t.Fatal("ApplyUI 应写出文件并生效")
	}
	if err := undo(); err != nil {
		t.Fatal(err)
	}
	if s.Initialized() || s.UI().ThemeMode != settings.ThemeSystem {
		t.Fatal("撤销应恢复成没有文件")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("撤销后文件应被删除")
	}

	// 文件已存在：只恢复被改的键，其他调用写入的偏好不受影响
	if err := s.Set("keep.pref", "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApplyUI(map[string]json.RawMessage{"fontSize": json.RawMessage(`"small"`)}); err != nil {
		t.Fatal(err)
	}
	undo, err = s.ApplyUI(map[string]json.RawMessage{"fontSize": json.RawMessage(`"large"`), "themeColor": json.RawMessage(`"green"`)})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set("during.pref", "y"); err != nil { // 撤销前别处写了偏好
		t.Fatal(err)
	}
	if err := undo(); err != nil {
		t.Fatal(err)
	}
	r := openUIPrefsStore(path, nil)
	ui := r.UI()
	if ui.FontSize != settings.FontSmall || ui.ThemeColor != settings.DefaultUI().ThemeColor {
		t.Fatalf("撤销应恢复被改的键：字号 %s，主题色 %s", ui.FontSize, ui.ThemeColor)
	}
	for _, key := range []string{"keep.pref", "during.pref"} {
		if _, ok := r.Get(key); !ok {
			t.Errorf("撤销不该影响偏好 %s", key)
		}
	}
}

func TestUIPrefsStoreWriteFailureLeavesMemoryUntouched(t *testing.T) {
	dir := t.TempDir()
	// config 位置被一个普通文件占着：创建目录与写入都会失败
	blocker := filepath.Join(dir, "config")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := openUIPrefsStore(filepath.Join(blocker, uiPrefsFileName), nil)
	if err := s.Set("k", 1); err == nil || !strings.Contains(err.Error(), uiPrefsFileName) {
		t.Fatalf("写入失败应返回带文件名的错误：%v", err)
	}
	if _, ok := s.Get("k"); ok {
		t.Fatal("写入失败后内存里不该留下新值")
	}
	if _, err := s.ApplyUI(map[string]json.RawMessage{"themeMode": json.RawMessage(`"dark"`)}); err == nil {
		t.Fatal("ApplyUI 写入失败应返回错误")
	}
	if s.UI().ThemeMode != settings.ThemeSystem || s.Initialized() {
		t.Fatal("写入失败后内存里不该留下新值")
	}
}

// 去抖保存在定时器的 goroutine 里并发调用 Set：文件始终是完整合法的，最后一次的值都在。
func TestUIPrefsStoreConcurrentSets(t *testing.T) {
	path := prefsPath(t)
	s := openUIPrefsStore(path, nil)
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := s.Set(fmt.Sprintf("k.%02d", i), i); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	r := openUIPrefsStore(path, nil)
	for i := 0; i < 24; i++ {
		raw, ok := r.Get(fmt.Sprintf("k.%02d", i))
		if !ok || string(raw) != fmt.Sprint(i) {
			t.Fatalf("k.%02d 丢了或不对：%s %v", i, raw, ok)
		}
	}
	if matches, _ := filepath.Glob(path + ".tmp-*"); len(matches) != 0 {
		t.Fatalf("不该残留临时文件：%v", matches)
	}
}
