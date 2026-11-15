package main

// 首次启动时，把旧版（React 渲染层）的设置与界面偏好导入原生界面的存储。
//
// 旧数据在 localStorage：WebView2 的 Local Storage（当前版本），或——用户直接从 Electron 版升上来、
// 中间没有运行过 Go 壳的 React 版——Electron 的 Local Storage（legacy_localstorage.go 的导入逻辑读它）。
// 解析规则（含持久化 v3 迁移与归一化）在 native/settings 的 ParseLegacy，由 TS 导出的 golden 对拍。
//
// 提交点是 ui-prefs.json 的写出：
//   - 读不了旧数据（损坏、复制失败）：返回错误、不写文件，下次启动重试；
//   - 没有旧数据（全新安装）或读到了：先把 Go 侧设置补进 settings 表，最后写文件，之后不再导入；
//   - 写表和写文件之间被打断：下次启动表里已有值（库里的值优先，不覆盖），重新走完剩下的步骤。
// 不会改动旧数据（只读复制），所以可以随时回退到旧版本。
//
// First-start import of the React UI's localStorage into the native UI's stores.

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"guizhi-mygo-shell/native/settings"
)

// readLegacyForNative 选出要导入的旧 localStorage：
//  1. WebView2 里有归知设置——最新的数据（Go 壳的 React 版首次启动时已把 Electron 的数据并进来）；
//  2. 否则 Electron 版还没被导入过（没有标记）且它有数据——直接从 Electron 版升上来；
//  3. 否则 WebView2 里零散的键（只有偏好、没有设置）；
//  4. 都没有：全新安装。
//
// 返回的 source 是 webview2 | electron；没有数据时 ls 为 nil。
func readLegacyForNative(webview2Folder, electronUserData string) (ls *legacyLocalStorage, source string, err error) {
	wv, err := readWebView2LocalStorage(webview2Folder)
	if err != nil {
		return nil, "", fmt.Errorf("读取 WebView2 里的旧设置失败：%w", err)
	}
	if wv != nil {
		if _, has := wv.Entries[legacyLSSettingsKey]; has {
			return wv, "webview2", nil
		}
	}
	if legacyImportPending(electronUserData) {
		el, err := readLegacyLocalStorage(electronUserData)
		if err != nil {
			return nil, "", fmt.Errorf("读取 Electron 版的旧设置失败：%w", err)
		}
		if el != nil {
			return el, "electron", nil
		}
	}
	if wv != nil {
		return wv, "webview2", nil
	}
	return nil, "", nil
}

// importLegacy 执行一次首次启动导入；文件已存在（已初始化）时什么都不做。
func (n *nativeSettings) importLegacy(ctx context.Context, webview2Folder, electronUserData string) error {
	if n.store.Initialized() {
		return nil
	}
	ls, source, err := readLegacyForNative(webview2Folder, electronUserData)
	if err != nil {
		return err
	}
	rec := uiPrefsImport{Source: "none", At: time.Now().UTC().Format(time.RFC3339)}
	var entries map[string]string
	if ls != nil {
		rec.Source, rec.Origin, rec.Keys = source, ls.Origin, len(ls.Entries)
		entries = ls.Entries
	}
	leg := settings.ParseLegacy(entries)
	rec.Issues = append(rec.Issues, leg.Issues...)
	rec.Issues = append(rec.Issues, n.importApp(ctx, leg.App)...)

	var ui map[string]json.RawMessage
	if leg.HasSettings {
		if ui, err = settings.Keys(leg.UI); err != nil {
			return fmt.Errorf("编码导入的界面设置失败：%w", err)
		}
	}
	if err := n.store.initialize(ui, leg.Prefs, rec); err != nil {
		return err
	}
	n.logf("[native-settings] 已从旧版导入：来源=%s，localStorage 键 %d 个，界面设置=%v，偏好 %d 项，问题 %d 条",
		rec.Source, rec.Keys, leg.HasSettings, len(leg.Prefs), len(rec.Issues))
	return nil
}

// importApp 把旧设置里属于 Go 侧的键补进 settings 表：库里已有值的键不动（库里的值优先）。
// 逐键写入，一个键非法（如 FlareSolverr 填错）不挡住别的键；返回被跳过的键的说明（不含值）。
func (n *nativeSettings) importApp(ctx context.Context, app map[string]json.RawMessage) (issues []string) {
	keys := make([]string, 0, len(app))
	for k := range app {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, exists := n.lib.readRawSetting(ctx, key); exists {
			continue
		}
		value := app[key]
		var parsed any
		_ = json.Unmarshal(value, &parsed)
		switch key {
		case "networkProxy":
			value, _ = json.Marshal(normalizeNetworkProxy(parsed))
		case "flareSolverr":
			flare, err := validateFlareSolverr(toAny(normalizeFlareSolverr(parsed)))
			if err != nil {
				issues = append(issues, fmt.Sprintf("%s: %v", key, err))
				continue
			}
			value, _ = json.Marshal(flare)
		}
		payload, err := json.Marshal(map[string]json.RawMessage{key: value})
		if err == nil {
			_, err = n.lib.settingsSet(ctx, payload)
		}
		if err != nil {
			issues = append(issues, fmt.Sprintf("%s: %v", key, err))
		}
	}
	return issues
}
