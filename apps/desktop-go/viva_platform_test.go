package main

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	vapp "github.com/vivaui/viva/app"
)

// 过滤器转换：后缀规范化、通配、兜底名称、跳过空后缀，且必须能通过 Viva 的校验（名称与模式非空、无 NUL）。
func TestToVivaFilters(t *testing.T) {
	cases := []struct {
		name string
		in   []dialogFilter
		want []vapp.FileFilter
	}{
		{"nil", nil, nil},
		{"普通后缀", []dialogFilter{{"Images", []string{"jpg", "png"}}}, []vapp.FileFilter{{Name: "Images", Pattern: "*.jpg;*.png"}}},
		{"前导点被去掉", []dialogFilter{{"MD", []string{".md", "..markdown"}}}, []vapp.FileFilter{{Name: "MD", Pattern: "*.md;*.markdown"}}},
		{"星号与空串都是全部文件", []dialogFilter{{"All Files", []string{"*"}}, {"Any", []string{""}}}, []vapp.FileFilter{{Name: "All Files", Pattern: "*.*"}, {Name: "Any", Pattern: "*.*"}}},
		{"名称为空兜底 Files", []dialogFilter{{"", []string{"txt"}}, {"  ", []string{"md"}}}, []vapp.FileFilter{{Name: "Files", Pattern: "*.txt"}, {Name: "Files", Pattern: "*.md"}}},
		{"没有后缀的过滤器跳过", []dialogFilter{{"Empty", nil}, {"Ok", []string{"exe"}}}, []vapp.FileFilter{{Name: "Ok", Pattern: "*.exe"}}},
		{"重复后缀去重", []dialogFilter{{"Dup", []string{"jpg", ".jpg", "JPG"}}}, []vapp.FileFilter{{Name: "Dup", Pattern: "*.jpg;*.JPG"}}},
		{"NUL 被剔除", []dialogFilter{{"A\x00B", []string{"x\x00y"}}}, []vapp.FileFilter{{Name: "AB", Pattern: "*.xy"}}},
	}
	for _, c := range cases {
		got := toVivaFilters(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s：got %+v want %+v", c.name, got, c.want)
		}
		for i, f := range got {
			if f.Name == "" || f.Pattern == "" || strings.ContainsRune(f.Name+f.Pattern, 0) {
				t.Errorf("%s：过滤器 %d 不合法：%+v", c.name, i, f)
			}
		}
	}
	// 产品里真实使用的过滤器全部转换成功（导入对话框的长后缀表）。
	if got := toVivaFilters(importFileFilters); len(got) != len(importFileFilters) {
		t.Errorf("导入过滤器应全部保留：%+v", got)
	}
}

func TestSplitSaveDefault(t *testing.T) {
	md := []dialogFilter{{"All", []string{"*"}}, {"Markdown", []string{"md"}}}
	cases := []struct {
		name     string
		path     string
		filters  []dialogFilter
		wantName string
		wantExt  string
	}{
		{"完整路径", `C:\Users\u\Downloads\备份.guizhi-backup`, nil, "备份.guizhi-backup", "guizhi-backup"},
		{"只有文件名", "export.html", nil, "export.html", "html"},
		{"空路径取过滤器首个非通配后缀", "", md, "", "md"},
		{"无后缀的文件名取过滤器后缀", `C:\x\notes`, md, "notes", "md"},
		{"目录分隔符结尾不当文件名", `C:\`, md, "", "md"},
		{"点号不当文件名", ".", nil, "", ""},
		{"都没有", "", nil, "", ""},
	}
	for _, c := range cases {
		name, ext := splitSaveDefault(c.path, c.filters)
		if name != c.wantName || ext != c.wantExt {
			t.Errorf("%s：got (%q, %q) want (%q, %q)", c.name, name, ext, c.wantName, c.wantExt)
		}
	}
}

// 假的 Viva 对话框：记下收到的选项，按预设返回。
type fakeDialogs struct {
	openOpts []vapp.OpenFileOptions
	dirOpts  []vapp.OpenDirectoryOptions
	saveOpts []vapp.SaveFileOptions
	paths    []string
	dir      string
	save     string
	err      error
}

func (f *fakeDialogs) api() vivaDialogs {
	return vivaDialogs{
		openFiles: func(o vapp.OpenFileOptions) ([]string, error) {
			f.openOpts = append(f.openOpts, o)
			return f.paths, f.err
		},
		openDirectory: func(o vapp.OpenDirectoryOptions) (string, error) {
			f.dirOpts = append(f.dirOpts, o)
			return f.dir, f.err
		},
		saveFile: func(o vapp.SaveFileOptions) (string, error) {
			f.saveOpts = append(f.saveOpts, o)
			return f.save, f.err
		},
	}
}

// 五个对话框入口都改走 Viva：选项正确传递，结果原样返回。
func TestWireVivaDialogsPassesOptions(t *testing.T) {
	f := &fakeDialogs{paths: []string{`C:\a.png`, `C:\b.png`}, dir: `C:\out`, save: `C:\out\x.md`}
	lib := &Library{}
	wireVivaDialogsWith(lib, f.api())

	got, err := lib.openDialog(true, "Images", []string{"jpg", "png"})
	if err != nil || !reflect.DeepEqual(got, f.paths) {
		t.Fatalf("openDialog = %v, %v", got, err)
	}
	if o := f.openOpts[0]; !o.AllowMultiple || len(o.Filters) != 1 || o.Filters[0].Pattern != "*.jpg;*.png" {
		t.Errorf("openDialog 选项 = %+v", o)
	}

	if _, err := lib.pickFiles(false, importFileFilters[:1]); err != nil {
		t.Fatal(err)
	}
	if o := f.openOpts[1]; o.AllowMultiple || len(o.Filters) != 1 {
		t.Errorf("pickFiles 选项 = %+v", o)
	}

	exe, err := lib.pickExecutable("选择 yt-dlp")
	if err != nil || exe != `C:\a.png` {
		t.Fatalf("pickExecutable = %q, %v", exe, err)
	}
	if o := f.openOpts[2]; o.Title != "选择 yt-dlp" || o.AllowMultiple || len(o.Filters) == 0 || o.Filters[len(o.Filters)-1].Pattern != "*.*" {
		t.Errorf("pickExecutable 选项 = %+v", o)
	}

	dir, err := lib.pickDirectory("选择导出目录")
	if err != nil || dir != `C:\out` || f.dirOpts[0].Title != "选择导出目录" {
		t.Fatalf("pickDirectory = %q, %v, %+v", dir, err, f.dirOpts)
	}

	p, err := lib.saveFileDialog("另存为", `C:\Users\u\a.md`, []dialogFilter{{"Markdown", []string{"md"}}})
	if err != nil || p != `C:\out\x.md` {
		t.Fatalf("saveFileDialog = %q, %v", p, err)
	}
	if o := f.saveOpts[0]; o.Title != "另存为" || o.SuggestedName != "a.md" || o.DefaultExtension != "md" || len(o.Filters) != 1 {
		t.Errorf("saveFileDialog 选项 = %+v", o)
	}
}

// 取消语义与原 mygo.Dialog 一致：用户取消返回空值且 err == nil（调用方据此判断「没选」，不弹错误）。
func TestWireVivaDialogsCancelIsEmptyNotError(t *testing.T) {
	for _, cancelErr := range []error{vapp.ErrCanceled, fmt.Errorf("包了一层：%w", vapp.ErrCanceled)} {
		f := &fakeDialogs{err: cancelErr}
		lib := &Library{}
		wireVivaDialogsWith(lib, f.api())

		if got, err := lib.openDialog(true, "Images", []string{"png"}); got != nil || err != nil {
			t.Errorf("openDialog 取消 = %v, %v", got, err)
		}
		if got, err := lib.pickFiles(true, importFileFilters); got != nil || err != nil {
			t.Errorf("pickFiles 取消 = %v, %v", got, err)
		}
		if got, err := lib.pickExecutable("x"); got != "" || err != nil {
			t.Errorf("pickExecutable 取消 = %q, %v", got, err)
		}
		if got, err := lib.pickDirectory("x"); got != "" || err != nil {
			t.Errorf("pickDirectory 取消 = %q, %v", got, err)
		}
		if got, err := lib.saveFileDialog("x", "a.md", nil); got != "" || err != nil {
			t.Errorf("saveFileDialog 取消 = %q, %v", got, err)
		}
	}
}

// 真实错误照常上抛，不能被当成取消吞掉。
func TestWireVivaDialogsRealErrorsPropagate(t *testing.T) {
	boom := errors.New("对话框打不开")
	f := &fakeDialogs{err: boom}
	lib := &Library{}
	wireVivaDialogsWith(lib, f.api())
	if _, err := lib.pickFiles(false, importFileFilters); !errors.Is(err, boom) {
		t.Errorf("pickFiles err = %v", err)
	}
	if _, err := lib.pickDirectory("x"); !errors.Is(err, boom) {
		t.Errorf("pickDirectory err = %v", err)
	}
	if _, err := lib.saveFileDialog("x", "", nil); !errors.Is(err, boom) {
		t.Errorf("saveFileDialog err = %v", err)
	}
	if _, err := lib.pickExecutable("x"); !errors.Is(err, boom) {
		t.Errorf("pickExecutable err = %v", err)
	}
}

// 没有注入系统外壳（单元测试）时，打开类入口返回错误、定位类什么都不做，绝不触发真实的系统窗口；注入后原样转调。
func TestLibraryShellFallbacksAndInjection(t *testing.T) {
	l := &Library{}
	if err := l.shellOpenExternal("https://example.com"); !errors.Is(err, errShellUnavailable) {
		t.Errorf("未注入的 openExternal err = %v", err)
	}
	if err := l.shellOpenPath(`C:\x`); !errors.Is(err, errShellUnavailable) {
		t.Errorf("未注入的 openPath err = %v", err)
	}
	l.shellShowItemInFolder(`C:\x`) // 不 panic

	var calls []string
	l.openExternal = func(s string) error { calls = append(calls, "ext:"+s); return nil }
	l.openPath = func(s string) error { calls = append(calls, "path:"+s); return nil }
	l.showItemInFolder = func(s string) { calls = append(calls, "show:"+s) }
	_ = l.shellOpenExternal("https://a")
	_ = l.shellOpenPath(`C:\p`)
	l.shellShowItemInFolder(`C:\s`)
	if want := []string{"ext:https://a", `path:C:\p`, `show:C:\s`}; !reflect.DeepEqual(calls, want) {
		t.Errorf("calls = %v", calls)
	}
}

// Viva 的外部链接入口只放行 http / https（来源链接来自采集到的网页）：不安全协议在到达系统之前就被拒绝。
func TestVivaExternalLinkRejectsUnsafeSchemes(t *testing.T) {
	lib := &Library{}
	wireVivaPlatform(lib)
	for _, u := range []string{"file:///C:/Windows/System32/calc.exe", "javascript:alert(1)", "ms-settings:network", "mailto:a@b.c", ""} {
		if err := lib.shellOpenExternal(u); err == nil {
			t.Errorf("%q 应被拒绝", u)
		}
	}
	if lib.openPath == nil || lib.showItemInFolder == nil {
		t.Error("wireVivaPlatform 应同时接好 openPath 与 showItemInFolder")
	}
}
