package main

// Viva 路径的平台能力接线：文件/目录对话框、外部链接、打开路径、资源管理器定位。
//
// Viva 路径下 mygo.App.Run() 不运行：mygo.Dialog.*、mygo.Shell.*、mygo.Screen.*、mygo.App.SetOpenAtLogin
// 都要把调用排进 mygo 的主线程循环，循环不存在时会 panic（needsApp）或永久等待。所以这里全部换成 Viva 的
// github.com/vivaui/viva/app 与直接的 Win32 调用，并保持与原 mygo 实现一致的取消语义：
// 用户取消返回空值且 err == nil（调用方据此判断「没选」，不弹错误）。

import (
	"errors"
	"path/filepath"
	"runtime"
	"strings"

	vapp "github.com/vivaui/viva/app"
)

// vivaDialogs 是 Viva 的文件对话框入口（vapp.OpenFiles 等满足；测试注入假实现，不弹真对话框）。
type vivaDialogs struct {
	openFiles     func(vapp.OpenFileOptions) ([]string, error)
	openDirectory func(vapp.OpenDirectoryOptions) (string, error)
	saveFile      func(vapp.SaveFileOptions) (string, error)
}

var realVivaDialogs = vivaDialogs{openFiles: vapp.OpenFiles, openDirectory: vapp.OpenDirectory, saveFile: vapp.SaveFile}

// wireVivaPlatform 把 Library 上所有依赖 mygo 事件循环的平台能力换成 Viva 实现（runViva 开头调用，早于任何对话框或链接请求）。
func wireVivaPlatform(lib *Library) {
	wireVivaDialogs(lib)
	lib.openExternal = webLinkOpener(vapp.OpenURL)
	lib.openPath = openPathNative
	lib.showItemInFolder = func(p string) { _ = vapp.RevealPath(p) }
}

// 以下三个是 Library 上系统外壳打开能力的统一入口（data_path.go 的 shell:openPath、updater_native.go 的更新器经它们调用）。
// 没有注入实现（单元测试）时给安全兜底：打开类返回错误，定位类什么都不做，绝不触发真实的系统窗口。
var errShellUnavailable = errors.New("系统外壳尚未接入")

func (l *Library) shellOpenExternal(url string) error {
	if l.openExternal == nil {
		return errShellUnavailable
	}
	return l.openExternal(url)
}

func (l *Library) shellOpenPath(path string) error {
	if l.openPath == nil {
		return errShellUnavailable
	}
	return l.openPath(path)
}

func (l *Library) shellShowItemInFolder(path string) {
	if l.showItemInFolder != nil {
		l.showItemInFolder(path)
	}
}

// wireVivaDialogs 用 Viva 的对话框替换 Library 的文件/目录选择与保存。
func wireVivaDialogs(lib *Library) { wireVivaDialogsWith(lib, realVivaDialogs) }

func wireVivaDialogsWith(lib *Library, d vivaDialogs) {
	lib.openDialog = func(multiple bool, name string, exts []string) ([]string, error) {
		return d.pickPaths(vapp.OpenFileOptions{AllowMultiple: multiple, Filters: toVivaFilters([]dialogFilter{{Name: name, Exts: exts}})})
	}
	lib.pickFiles = func(multiple bool, filters []dialogFilter) ([]string, error) {
		return d.pickPaths(vapp.OpenFileOptions{AllowMultiple: multiple, Filters: toVivaFilters(filters)})
	}
	lib.pickExecutable = func(title string) (string, error) {
		filters := []dialogFilter{{Name: "All Files", Exts: []string{"*"}}}
		if runtime.GOOS == "windows" {
			filters = append([]dialogFilter{{Name: "可执行文件", Exts: []string{"exe"}}}, filters...)
		}
		paths, err := d.pickPaths(vapp.OpenFileOptions{Title: title, Filters: toVivaFilters(filters)})
		if err != nil || len(paths) == 0 {
			return "", err
		}
		return paths[0], nil
	}
	lib.pickDirectory = func(title string) (string, error) {
		p, err := d.openDirectory(vapp.OpenDirectoryOptions{Title: title})
		if errors.Is(err, vapp.ErrCanceled) {
			return "", nil
		}
		return p, err
	}
	lib.saveFileDialog = func(title, defaultPath string, filters []dialogFilter) (string, error) {
		name, ext := splitSaveDefault(defaultPath, filters)
		p, err := d.saveFile(vapp.SaveFileOptions{Title: title, SuggestedName: name, DefaultExtension: ext, Filters: toVivaFilters(filters)})
		if errors.Is(err, vapp.ErrCanceled) {
			return "", nil
		}
		return p, err
	}
}

// pickPaths 打开「选择文件」；用户取消返回 (nil, nil)，与 mygo.Dialog.Open 一致。
func (d vivaDialogs) pickPaths(o vapp.OpenFileOptions) ([]string, error) {
	paths, err := d.openFiles(o)
	if errors.Is(err, vapp.ErrCanceled) {
		return nil, nil
	}
	return paths, err
}

// toVivaFilters 把按扩展名描述的过滤器换成 Windows COMDLG_FILTERSPEC 的模式串（"*.jpg;*.png"）：
// 扩展名去前导「.」，空串或 "*" 表示全部文件（"*.*"）；名称为空取 "Files"；没有任何扩展名的过滤器跳过。
// 结果保证能通过 vapp 的 validateFilters（名称与模式都非空、不含 NUL）。
func toVivaFilters(fs []dialogFilter) []vapp.FileFilter {
	var out []vapp.FileFilter
	for _, f := range fs {
		var pats []string
		seen := map[string]bool{}
		for _, e := range f.Exts {
			e = strings.TrimLeft(strings.TrimSpace(strings.ReplaceAll(e, "\x00", "")), ".")
			pat := "*." + e
			if e == "" || e == "*" {
				pat = "*.*"
			}
			if !seen[pat] {
				seen[pat] = true
				pats = append(pats, pat)
			}
		}
		if len(pats) == 0 {
			continue
		}
		name := strings.TrimSpace(strings.ReplaceAll(f.Name, "\x00", ""))
		if name == "" {
			name = "Files"
		}
		out = append(out, vapp.FileFilter{Name: name, Pattern: strings.Join(pats, ";")})
	}
	return out
}

// splitSaveDefault 把 mygo 风格的 DefaultPath 拆成 Viva 的建议文件名与默认扩展名：
// 文件名取路径末段（排除 "." 与分隔符）；扩展名取文件名的后缀（去前导「.」），没有时取第一个非通配的过滤器后缀。
func splitSaveDefault(defaultPath string, filters []dialogFilter) (name, ext string) {
	if defaultPath != "" {
		base := filepath.Base(defaultPath)
		if base != "." && base != ".." && !strings.ContainsAny(base, `/\`) {
			name = base
			ext = strings.TrimPrefix(filepath.Ext(base), ".")
		}
	}
	if ext == "" {
		for _, f := range filters {
			for _, e := range f.Exts {
				if e = strings.TrimLeft(strings.TrimSpace(e), "."); e != "" && e != "*" {
					return name, e
				}
			}
		}
	}
	return name, ext
}
