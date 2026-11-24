package main

// 背景图适配器（native_settings_appearance.go）的集成测试：真实的 IPC 处理器（app_runtime.go）+ 临时数据目录
// （不碰 %APPDATA%\GuiZhi）+ 假的选图对话框。

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// backgroundEnv 是一套隔离环境：临时用户数据目录、登记了图片处理器的 Bridge、可编排的选图对话框。
type backgroundEnv struct {
	lib    *Library
	svc    *backgroundImagesAdapter
	srcDir string
	// dialog 决定下一次选图的结果；nil 时返回取消。
	dialog func() ([]string, error)
}

func newBackgroundEnv(t *testing.T) *backgroundEnv {
	t.Helper()
	ud := filepath.Join(t.TempDir(), "GuiZhi")
	e := &backgroundEnv{srcDir: t.TempDir()}
	e.lib = &Library{paths: Paths{UserData: ud}}
	e.lib.openDialog = func(bool, string, []string) ([]string, error) {
		if e.dialog == nil {
			return []string{}, nil
		}
		return e.dialog()
	}
	b := newSystemBridge()
	e.lib.registerAppRuntime(b)
	e.svc = newNativeServices(b, newPushHub()).BackgroundImages.(*backgroundImagesAdapter)
	if err := os.MkdirAll(e.lib.paths.ImagesDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *backgroundEnv) source(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(e.srcDir, name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func (e *backgroundEnv) saved(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(e.lib.paths.ImagesDir())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, en := range entries {
		names = append(names, en.Name())
	}
	return names
}

var uuidNameRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.png$`)

// newNativeServices 把背景图适配器接上；没有它，设置页的「选择图片」会一直提示「宿主没接上选图」。
func TestNativeSettingsAppearanceAssembled(t *testing.T) {
	if newNativeServices(newSystemBridge(), newPushHub()).BackgroundImages == nil {
		t.Fatal("newNativeServices 应装配 BackgroundImages")
	}
}

// 用户取消选择：返回空文件名且没有错误，图片目录里什么也没有多出来。
func TestNativeSettingsAppearancePickCancel(t *testing.T) {
	e := newBackgroundEnv(t)
	name, err := e.svc.PickAndSave(context.Background())
	if name != "" || err != nil {
		t.Fatalf("取消应是 (\"\", nil)：%q %v", name, err)
	}
	if got := e.saved(t); len(got) != 0 {
		t.Fatalf("取消不应存任何文件：%v", got)
	}
}

// 正常选图与存图：返回应用图片目录里的纯文件名（UUID 加原扩展名），内容与原图逐字节相同，之后能读回来。
func TestNativeSettingsAppearancePickSaveRead(t *testing.T) {
	e := newBackgroundEnv(t)
	data := []byte{0x89, 'P', 'N', 'G', 1, 2, 3, 4, 0, 255}
	src := e.source(t, "My Photo.PNG", data)
	e.dialog = func() ([]string, error) { return []string{src}, nil }

	// 扩展名大写：image:save 保留原扩展名的大小写，所以这里只校验 UUID 形状加扩展名（忽略大小写）。
	name, err := e.svc.PickAndSave(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !uuidNameRe.MatchString(strings.ToLower(name)) || strings.ContainsAny(name, `/\`) {
		t.Fatalf("应返回 UUID 加扩展名的纯文件名：%q", name)
	}
	onDisk, err := os.ReadFile(filepath.Join(e.lib.paths.ImagesDir(), name))
	if err != nil || !bytes.Equal(onDisk, data) {
		t.Fatalf("应用图片目录里应有逐字节相同的拷贝：%v", err)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("原图不应被移动或删除：%v", err)
	}
	got, err := e.svc.Read(context.Background(), name)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("读回应与原图相同：%v", err)
	}
}

// 多选时只存第一张。
func TestNativeSettingsAppearancePickSavesOnlyTheFirst(t *testing.T) {
	e := newBackgroundEnv(t)
	first := e.source(t, "a.png", []byte("first"))
	second := e.source(t, "b.png", []byte("second"))
	e.dialog = func() ([]string, error) { return []string{first, second}, nil }
	name, err := e.svc.PickAndSave(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := e.saved(t); len(got) != 1 || got[0] != name {
		t.Fatalf("应只存了第一张：%v", got)
	}
	if data, _ := e.svc.Read(context.Background(), name); string(data) != "first" {
		t.Fatalf("存下的应是第一张：%q", data)
	}
}

// 选中了路径但没存出文件名（扩展名不在白名单、原文件读不出来）：返回错误，不能当作取消。
func TestNativeSettingsAppearancePickSaveFailures(t *testing.T) {
	e := newBackgroundEnv(t)
	cases := map[string]string{
		"svg 不是位图":    e.source(t, "logo.svg", []byte("<svg/>")),
		"没有扩展名":       e.source(t, "noext", []byte("data")),
		"选中的文件在保存前消失": filepath.Join(e.srcDir, "gone.png"),
	}
	for label, path := range cases {
		e.dialog = func() ([]string, error) { return []string{path}, nil }
		name, err := e.svc.PickAndSave(context.Background())
		if err == nil || name != "" {
			t.Errorf("%s：应返回错误：%q %v", label, name, err)
		}
	}
	if got := e.saved(t); len(got) != 0 {
		t.Errorf("失败时不应留下文件：%v", got)
	}
}

// 对话框本身失败（含没接上对话框）：错误原样带回。
func TestNativeSettingsAppearancePickDialogErrors(t *testing.T) {
	e := newBackgroundEnv(t)
	boom := errors.New("dialog exploded")
	e.dialog = func() ([]string, error) { return nil, boom }
	if _, err := e.svc.PickAndSave(context.Background()); err == nil || !strings.Contains(err.Error(), "dialog exploded") {
		t.Fatalf("对话框的错误应带回：%v", err)
	}
	e.lib.openDialog = nil
	if _, err := e.svc.PickAndSave(context.Background()); err == nil {
		t.Fatal("没接上对话框应返回错误")
	}
}

// image:save 只认刚经过选图框登记的路径：没选过的路径直接调用，处理器返回空列表、不写文件。
// 适配器因此不可能被用来把任意磁盘文件拷进应用图片目录。
func TestNativeSettingsAppearanceSaveRejectsUnpickedPath(t *testing.T) {
	e := newBackgroundEnv(t)
	src := e.source(t, "secret.png", []byte("not picked"))
	b := newSystemBridge()
	e.lib.registerAppRuntime(b)
	saved, err := callAs[[]string](context.Background(), b, "image:save", []string{src})
	if err != nil || len(saved) != 0 {
		t.Fatalf("没经过选图框的路径应被拒绝：%v %v", saved, err)
	}
	if got := e.saved(t); len(got) != 0 {
		t.Fatalf("不应写入文件：%v", got)
	}
}

// 读取：路径穿越、子目录、非图片扩展名、保留设备名与不存在的文件全部返回明确的错误，绝不静默返回空字节。
func TestNativeSettingsAppearanceReadRejects(t *testing.T) {
	e := newBackgroundEnv(t)
	dir := e.lib.paths.ImagesDir()
	if err := os.WriteFile(filepath.Join(dir, "vector.svg"), []byte("<svg/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "empty.png"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "folder.png"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "x.png"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(filepath.Dir(dir), "outside.png")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"", ".", "..", "../outside.png", `..\outside.png`, "sub/x.png", `sub\x.png`, "/abs.png", `C:\x.png`, "C:x.png",
		"vector.svg", "noext", "missing.png", "empty.png", "folder.png", "con.png", "trailing.png ", "dot.png.", "nul\x00.png",
	} {
		data, err := e.svc.Read(context.Background(), name)
		if err == nil || len(data) != 0 {
			t.Errorf("Read(%q) 应返回错误且没有字节：%d 字节 %v", name, len(data), err)
		}
	}
}

// 读取：大小写不敏感的扩展名白名单，五种位图扩展名都放行。
func TestNativeSettingsAppearanceReadAcceptsBitmapExtensions(t *testing.T) {
	e := newBackgroundEnv(t)
	for i, name := range []string{"a.png", "b.JPG", "c.jpeg", "d.gif", "e.WebP"} {
		payload := []byte{byte(i + 1), 7, 7}
		if err := os.WriteFile(filepath.Join(e.lib.paths.ImagesDir(), name), payload, 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := e.svc.Read(context.Background(), name)
		if err != nil || !bytes.Equal(got, payload) {
			t.Errorf("Read(%q)：%v %v", name, got, err)
		}
	}
}

// 并发选图（-race）：选图与存图被串行化，每次保存的内容都是自己选的那张，不会被另一次选图冲掉登记。
func TestNativeSettingsAppearanceConcurrentPicks(t *testing.T) {
	e := newBackgroundEnv(t)
	const n = 6
	var mu sync.Mutex
	next := 0
	paths := make([]string, n)
	for i := range paths {
		paths[i] = e.source(t, "p"+string(rune('a'+i))+".png", []byte{byte(i), 9, 9})
	}
	e.dialog = func() ([]string, error) {
		mu.Lock()
		defer mu.Unlock()
		p := paths[next%n]
		next++
		return []string{p}, nil
	}
	var wg sync.WaitGroup
	names := make([]string, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			names[i], errs[i] = e.svc.PickAndSave(context.Background())
		}()
	}
	wg.Wait()
	seen := map[byte]bool{}
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("#%d：%v", i, errs[i])
		}
		data, err := e.svc.Read(context.Background(), names[i])
		if err != nil || len(data) != 3 {
			t.Fatalf("#%d 读回失败：%v %v", i, data, err)
		}
		seen[data[0]] = true
	}
	if len(seen) != n {
		t.Fatalf("每次选图应各存各的：%v", seen)
	}
}
