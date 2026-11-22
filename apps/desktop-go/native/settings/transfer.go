package settings

import (
	"encoding/json"
	"fmt"
)

// 配置迁移（config:export / config:apply）与原生设置之间的换算：纯函数，不读写任何存储。
//
// 配置文件沿用渲染层的信封：`settings` 是旧 guizhi-settings 的 state（键名同 UI 与 App 的 JSON 标签），
// `uiLayout` 是旧 localStorage 里 ui-storage 与列配置两项。原生界面没有 localStorage，
// 所以导出时从 Values 与 Prefs 拼出这两份，导入时再按旧格式解析（ParseLegacy 做归一与校验）后盖回去——
// 文件格式只有一个，渲染层导出的配置原生能导入，反之亦然。

// TransferLayoutKeys 是随配置走的界面偏好：窗格宽度、侧栏折叠、知识库视图模式与列配置。
// 不含 PrefAppModule：它是「上次停在哪个模块」，属于导航状态，不是布局，导入配置不该把用户搬到别的模块。
func TransferLayoutKeys() []string {
	keys := []string{PrefSidebarCollapsed, PrefLibraryViewMode, PrefLibraryColumns}
	for _, p := range PaneWidths {
		keys = append(keys, p.Key)
	}
	return keys
}

// TransferState 把设置编码成导出文件里的 settings 记录：UI 与 App 的全部键。
// 机器绑定的键（路径、开机自启、壁纸文件名）由 config:export 统一剔除，机密由它按口令加密或清空。
func TransferState(v Values) (map[string]json.RawMessage, error) {
	state, err := Keys(v.UI)
	if err != nil {
		return nil, err
	}
	app, err := Keys(v.App)
	if err != nil {
		return nil, err
	}
	for k, raw := range app {
		state[k] = raw
	}
	return state, nil
}

// TransferLayout 把界面偏好编码成导出文件里的 uiLayout（旧 localStorage 键 → 值）；一项偏好都没有时返回空表。
// get 读一项偏好（settings.Pref* 键）。
func TransferLayout(get func(key string) (json.RawMessage, bool)) map[string]any {
	out := map[string]any{}
	state := map[string]any{}
	set := func(field, key string) {
		if raw, ok := get(key); ok {
			state[field] = raw
		}
	}
	set("isSidebarCollapsed", PrefSidebarCollapsed)
	set("libraryViewMode", PrefLibraryViewMode)
	for _, p := range PaneWidths {
		set(p.LegacyUIField, p.Key)
	}
	if len(state) > 0 {
		// zustand persist 没写 version 时是 0。
		out["ui-storage"] = map[string]any{"state": state, "version": 0}
	}
	if raw, ok := get(PrefLibraryColumns); ok {
		out["guizhi-library-table-config"] = raw
	}
	return out
}

// ImportedValues 把导入结果里的 settings 记录（旧 state）盖在 base 之上：只有记录里出现的键才改，
// 其余保持原值——机器绑定的几项（工具路径、开机自启、壁纸）不在文件里，整份替换会让它们退回默认值。
// 取值按渲染层规则归一（DecodeUI、legacyApp）；类型不对的键保持原值。网络代理与 FlareSolverr 整块替换，
// 细部规则由写入处的 Go 侧归一化负责。可执行文件路径不导入（只接受文件选择器返回过的值）。
func ImportedValues(base Values, state map[string]json.RawMessage, version int) (Values, error) {
	envelope, err := json.Marshal(map[string]any{"state": state, "version": version})
	if err != nil {
		return base, fmt.Errorf("编码导入的设置失败：%w", err)
	}
	legacy := ParseLegacy(map[string]string{LegacySettingsKey: string(envelope)})
	if !legacy.HasSettings {
		return base, fmt.Errorf("导入的设置内容无法解析")
	}

	next := base.Clone()

	normalizedUI, err := Keys(legacy.UI)
	if err != nil {
		return base, err
	}
	merged, err := Keys(next.UI)
	if err != nil {
		return base, err
	}
	for k := range state {
		if v, ok := normalizedUI[k]; ok {
			merged[k] = v
		}
	}
	// 解码进全新的值：json.Unmarshal 进已有的 map 是合并，旧的场景模型项会漏过「以文件为准」。
	var ui UI
	if err := overlayJSON(merged, &ui); err != nil {
		return base, err
	}
	next.UI = ui

	app, err := Keys(next.App)
	if err != nil {
		return base, err
	}
	for k, v := range legacy.App {
		app[k] = v
	}
	var appValues App
	if err := overlayJSON(app, &appValues); err != nil {
		return base, err
	}
	next.App = appValues
	return next, nil
}

// overlayJSON 把键值表按 JSON 标签解码进 dst。
func overlayJSON(keys map[string]json.RawMessage, dst any) error {
	raw, err := json.Marshal(keys)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("导入的设置类型不符：%w", err)
	}
	return nil
}

// ImportedPrefs 把导入结果里的 uiLayout 换成要写入的界面偏好（键 → JSON）：只含 TransferLayoutKeys 里的项，
// 且只含文件里存在且结构有效的项（坏数据被丢弃，不影响其余）。
func ImportedPrefs(layout map[string]json.RawMessage) map[string]json.RawMessage {
	entries := make(map[string]string, len(layout))
	for k, raw := range layout {
		entries[k] = string(raw)
	}
	parsed := ParseLegacy(entries).Prefs
	out := map[string]json.RawMessage{}
	for _, key := range TransferLayoutKeys() {
		if raw, ok := parsed[key]; ok {
			out[key] = raw
		}
	}
	return out
}
