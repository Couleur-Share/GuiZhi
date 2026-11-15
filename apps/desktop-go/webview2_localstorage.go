package main

// 读取 WebView2（React 渲染层）的 localStorage，供原生界面首次启动时导入设置与偏好（native_settings_import.go）。
//
// 位置与 MyGo 的取法一致：环境变量 WEBVIEW2_USER_DATA_FOLDER，没有就是 %LOCALAPPDATA%\<应用名>\WebView2；
// Local Storage 在其下的 EBWebView\Default\Local Storage\leveldb，页面 origin 是 rendererOrigin。
// 这些事实由真实 WebView2 写出的夹具核对（testdata/webview2-localstorage 的 meta.json，生成脚本见
// testdata/make-webview2-localstorage.mjs），内核升级后若位置变了，测试会先红。
//
// Reads the localStorage of the WebView2 profile the React UI used, so the native UI can import it on first start.

import (
	"os"
	"path/filepath"
	"sort"
)

// webview2AppName 与 mygo.json 的 name 一致（测试核对）：MyGo 用它决定 %LOCALAPPDATA% 下的目录名。
const webview2AppName = "GuiZhi"

// webview2LocalStorageRel 是 Local Storage 的 leveldb 相对 WebView2 用户数据目录的路径。
var webview2LocalStorageRel = filepath.Join("EBWebView", "Default", "Local Storage", "leveldb")

// sanitizeWebView2Name 与 MyGo internal/windows 的 sanitize 一致：非法文件名字符换成下划线，空名取 MyGo。
func sanitizeWebView2Name(name string) string {
	out := []rune{}
	for _, r := range name {
		if r < ' ' || r == '<' || r == '>' || r == ':' || r == '"' || r == '/' || r == '\\' || r == '|' || r == '?' || r == '*' {
			r = '_'
		}
		out = append(out, r)
	}
	if len(out) == 0 {
		return "MyGo"
	}
	return string(out)
}

// webview2UserDataFolder 返回 WebView2 的用户数据目录，取法与 MyGo 一致：
// WEBVIEW2_USER_DATA_FOLDER（相对路径相对于可执行文件所在目录），否则 %LOCALAPPDATA%\<应用名>\WebView2。
func webview2UserDataFolder(getenv func(string) string, exeDir func() string) string {
	if dir := getenv("WEBVIEW2_USER_DATA_FOLDER"); dir != "" {
		if filepath.IsAbs(dir) {
			return dir
		}
		return filepath.Join(exeDir(), dir)
	}
	local := getenv("LOCALAPPDATA")
	if local == "" {
		local = os.TempDir()
	}
	return filepath.Join(local, sanitizeWebView2Name(webview2AppName), "WebView2")
}

// currentWebView2UserDataFolder 按当前进程的环境取 WebView2 用户数据目录。
func currentWebView2UserDataFolder() string {
	return webview2UserDataFolder(os.Getenv, func() string {
		exe, err := os.Executable()
		if err != nil {
			return ""
		}
		return filepath.Dir(exe)
	})
}

// readWebView2LocalStorage 读取 WebView2 用户数据目录里的归知 localStorage。没有数据返回 (nil, nil)；
// 读不了（损坏等）返回错误，调用方应记录后继续启动。
func readWebView2LocalStorage(userDataFolder string) (*legacyLocalStorage, error) {
	byOrigin, err := readLocalStorageLevelDB(filepath.Join(userDataFolder, webview2LocalStorageRel), "WebView2 Local Storage")
	if err != nil || byOrigin == nil {
		return nil, err
	}
	return pickWebView2Origin(byOrigin), nil
}

// pickWebView2Origin 取归知渲染层页面的 origin；找不到时取任何带归知设置的 origin（例如开发服务器），
// 其余无关来源一律不导入（同一个 WebView2 配置目录可能还装着别的页面的数据）。
func pickWebView2Origin(byOrigin map[string]map[string]string) *legacyLocalStorage {
	if m, ok := byOrigin[rendererOrigin]; ok && len(m) > 0 {
		return &legacyLocalStorage{Origin: rendererOrigin, Entries: m}
	}
	origins := make([]string, 0, len(byOrigin))
	for o := range byOrigin {
		origins = append(origins, o)
	}
	sort.Strings(origins)
	for _, o := range origins {
		if _, has := byOrigin[o][legacyLSSettingsKey]; has {
			return &legacyLocalStorage{Origin: o, Entries: byOrigin[o]}
		}
	}
	return nil
}
